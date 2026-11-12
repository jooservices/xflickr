import type { SetupPage, SetupPageEvent, SetupPageId } from './types';
import type { WizardRoute } from './types';

export function indexPages(registry: readonly SetupPage[]): Map<SetupPageId, SetupPage> {
  const pages = new Map<SetupPageId, SetupPage>();
  for (const page of registry) {
    if (pages.has(page.id)) throw new Error(`Duplicate setup page "${page.id}" in registry.`);
    pages.set(page.id, page);
  }
  return pages;
}

export function requirePage(
  pages: Map<SetupPageId, SetupPage>,
  id: SetupPageId,
  source: SetupPageId,
): SetupPage {
  const page = pages.get(id);
  if (!page) throw new Error(`Unknown setup page "${id}" referenced by "${source}".`);
  return page;
}

export function transition(
  pages: Map<SetupPageId, SetupPage>,
  current: WizardRoute,
  event: SetupPageEvent,
): WizardRoute {
  if (!current.pageId) return current;
  const source = requirePage(pages, current.pageId, 'current page');
  const target = transitionTarget(source, event);
  const destination = requirePage(pages, target.id, source.id);
  return {
    pageId: destination.id,
    failure: target.failure,
    payload: target.payload,
    hostError: null,
    revision: current.revision + 1,
  };
}

function transitionTarget(
  page: SetupPage,
  event: SetupPageEvent,
): { id: SetupPageId; failure: string | null; payload: unknown } {
  if (event.type === 'success')
    return { id: page.next, failure: null, payload: event.payload ?? null };
  if (event.type === 'failure')
    return { id: page.onFail, failure: event.error ?? 'error', payload: null };
  if (!page.rollbackTo.includes(event.target)) {
    throw new Error(`Setup page "${page.id}" cannot roll back to "${event.target}".`);
  }
  return { id: event.target, failure: null, payload: null };
}
