import type { ReactElement } from 'react';
import { matchRoutes } from 'react-router';
import { describe, expect, it } from 'vitest';
import { adminSession, renderApp, userSession } from '../../test/render';
import { routes } from '../router';
import { ADMIN_NAV, USER_NAV } from '../shells/nav';
import { PlaceholderPage } from '../../pages';
import { HomePage } from '../../pages/home/HomePage';
import { LibraryPage } from '../../pages/library';
import { ProfilePage } from '../../pages/profile/ProfilePage';
import { ContactsPage } from '../../pages/contacts/ContactsPage';
import { CrawlPage } from '../../pages/crawl';
import { AlbumsPage } from '../../pages/albums';
import { KeysPage } from '../../pages/keys';
import { SetupPage } from '../../pages/setup/SetupPage';
import { DefaultsPage } from '../../pages/admin/defaults/DefaultsPage';
import { NodesPage } from '../../pages/admin/nodes/NodesPage';
import { QuotaPage } from '../../pages/admin/quota';
import { IssuesPage } from '../../pages/admin/issues';
import { OverviewPage } from '../../pages/admin/overview/OverviewPage';

/** The element of the deepest route that matches `path` in the composed route table. */
function leafElement(path: string): ReactElement {
  const matches = matchRoutes(routes, path) ?? [];
  const leaf = matches.at(-1);
  if (!leaf) throw new Error(`no route matches ${path}`);
  return leaf.route.element as ReactElement;
}

/** User pages that have their own component; every other USER_NAV path is a placeholder. */
const USER_PAGES: Record<string, unknown> = {
  '/': HomePage,
  '/library': LibraryPage,
  '/profile': ProfilePage,
  '/contacts': ContactsPage,
  '/crawl': CrawlPage,
  '/albums': AlbumsPage,
  '/keys': KeysPage,
};

/** Admin pages by ADMIN_NAV path; every other admin path is a placeholder. */
const ADMIN_PAGES: Record<string, unknown> = {
  '/admin': OverviewPage,
  '/admin/nodes': NodesPage,
  '/admin/quota': QuotaPage,
  '/admin/issues': IssuesPage,
  '/admin/defaults': DefaultsPage,
};

describe('composed route table', () => {
  it.each(USER_NAV)('resolves user path $path to the same page as before', (item) => {
    const element = leafElement(item.path);
    const page = USER_PAGES[item.path];

    if (page) {
      expect(element.type).toBe(page);
      return;
    }
    expect(element.type).toBe(PlaceholderPage);
    expect(element.props).toMatchObject({
      scope: 'shell',
      titleKey: item.labelKey,
      unavailable: true,
    });
  });

  it.each(ADMIN_NAV)('resolves admin id $id to the same page as before', (item) => {
    const element = leafElement(item.path);
    const page = ADMIN_PAGES[item.path];

    if (page) {
      expect(element.type).toBe(page);
      return;
    }
    expect(element.type).toBe(PlaceholderPage);
    expect(element.props).toMatchObject({ scope: 'admin', titleKey: item.labelKey });
  });

  it('keeps the extra user and admin paths that are not in the navigation', () => {
    expect(leafElement('/contacts/3').type).toBe(PlaceholderPage);
    expect(leafElement('/albums/3').type).toBe(PlaceholderPage);
    expect(leafElement('/accounts').type).toBe(PlaceholderPage);
    expect(leafElement('/sessions').type).toBe(PlaceholderPage);
    expect(leafElement('/onboarding').type).toBe(SetupPage);
    expect(leafElement('/admin/nodes/7').props).toMatchObject({
      scope: 'admin',
      titleKey: 'admin.nav.nodes',
    });
  });

  it('renders the 404 page for an unknown user path', async () => {
    const view = renderApp({ session: userSession, path: '/no-such-page' });

    expect(await view.findByTestId('shell-notfound')).toBeInTheDocument();
  });

  it('renders the 404 page for an unknown admin path', async () => {
    const view = renderApp({ session: adminSession, path: '/admin/no-such-page' });

    expect(await view.findByTestId('shell-notfound')).toBeInTheDocument();
  });
});
