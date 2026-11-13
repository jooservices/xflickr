import type { ApiKey, FlickrAccount } from '../../shared/api/gen/models';
import type { SetupPageId } from './types';

export type SetupEntryData = { keys: ApiKey[]; accounts: FlickrAccount[] };
export type SetupEntryUrl = string | URL | URLSearchParams;

/** Resolves the wizard entry after a Flickr redirect or a fresh server load. */
export function resolveEntry(data: SetupEntryData, url: SetupEntryUrl): SetupPageId {
  const params = parametersOf(url);
  if (hasConnectResult(params)) return 'connect-result';
  if (data.accounts.length > 0) return 'done';

  const hasValidKey = data.keys.some((key) => key.status === 'valid');
  // Keep the legacy resume URL: a valid key with step=3 opens the empty done page.
  if (hasValidKey && params.get('step') === '3') return 'done';
  return hasValidKey ? 'connect' : 'key';
}

function parametersOf(url: SetupEntryUrl): URLSearchParams {
  if (url instanceof URLSearchParams) return url;
  if (url instanceof URL) return url.searchParams;
  return new URL(url, 'http://localhost').searchParams;
}

function hasConnectResult(params: URLSearchParams): boolean {
  return params.has('connected') || params.has('connect_error') || params.has('error');
}
