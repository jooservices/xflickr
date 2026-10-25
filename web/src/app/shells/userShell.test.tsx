import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw/http';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { apiKeyBody } from '../../test/fixtures';
import { server } from '../../test/server';
import { adminSession, renderApp, userSession } from '../../test/render';

import { USER_NAV } from './nav';

beforeEach(() => {
  window.localStorage.clear();
});

describe('user shell', () => {
  it('shows the 11 tabs in order, each linking to its placeholder page', async () => {
    renderApp({ session: userSession });

    const nav = await screen.findByTestId('shell-nav');
    const links = within(nav).getAllByRole('link');
    expect(links.map((link) => link.textContent?.trim())).toContain('Keys & quota');
    expect(links).toHaveLength(11);
    expect(links.map((link) => link.getAttribute('href'))).toEqual(
      USER_NAV.map((item) => item.path),
    );
    expect(screen.getByTestId('shell-nav-home')).toHaveAttribute('aria-current', 'page');
  });

  it.each(USER_NAV.filter((item) => item.path !== '/'))(
    'routes $id to a page and marks the tab current',
    async (item) => {
      renderApp({ session: userSession, path: item.path });

      expect(await screen.findByRole('heading', { level: 1 })).toBeInTheDocument();
      expect(screen.getByTestId(`shell-nav-${item.id}`)).toHaveAttribute('aria-current', 'page');
      if (item.id === 'profile') {
        expect(screen.getByTestId('profile-page')).toBeInTheDocument();
      } else if (item.id === 'crawl') {
        expect(screen.getByTestId('user-crawl-page')).toBeInTheDocument();
      } else if (item.id === 'keys') {
        expect(screen.getByTestId('keys-page')).toBeInTheDocument();
      } else {
        expect(screen.getByTestId('shell-page')).toBeInTheDocument();
      }
      expect(document.title).toMatch(/ · XFlickr$/);
    },
  );

  it('moves focus to the page heading after a route change', async () => {
    renderApp({ session: userSession });

    fireEvent.click(await screen.findByTestId('shell-nav-library'));

    await waitFor(() => {
      expect(screen.getByRole('heading', { level: 1, name: 'Library' })).toHaveFocus();
    });
  });

  it('keeps Contacts active on a contact page and shows the back crumb', async () => {
    renderApp({ session: userSession, path: '/contacts/42' });

    expect(await screen.findByTestId('shell-nav-contacts')).toHaveAttribute('aria-current', 'page');
    expect(screen.getByTestId('shell-crumb')).toHaveAttribute('href', '/contacts');
  });

  it('uses the origin contact for an album crumb when given', async () => {
    const { router } = renderApp({ session: userSession, path: '/albums/7' });
    expect(await screen.findByTestId('shell-crumb')).toHaveAttribute('href', '/albums');

    await router.navigate('/albums/8', { state: { from: { path: '/contacts/3', name: 'Anna' } } });
    await waitFor(() => {
      expect(screen.getByTestId('shell-crumb')).toHaveAttribute('href', '/contacts/3');
    });
    expect(screen.getByTestId('shell-crumb')).toHaveTextContent('Anna');
  });

  it('hides the tab row on onboarding', async () => {
    renderApp({ session: userSession, path: '/onboarding' });

    await screen.findByTestId('wizard-page');
    expect(screen.queryByTestId('shell-nav')).toBeNull();
  });

  it('shows the 404 page for an unknown route', async () => {
    renderApp({ session: userSession, path: '/nope' });

    expect(await screen.findByTestId('shell-notfound')).toBeInTheDocument();
    fireEvent.click(screen.getByTestId('shell-notfound-home'));
    expect(await screen.findByTestId('shell-page')).toBeInTheDocument();
  });

  it('switches EN/VI at once and updates <html lang>', async () => {
    renderApp({ session: userSession });
    const toggle = await screen.findByTestId('shell-lang-toggle');
    expect(toggle).toHaveAccessibleName('Switch to Tiếng Việt');

    fireEvent.click(toggle);

    expect(await screen.findByTestId('shell-nav-library')).toHaveTextContent('Thư viện');
    expect(document.documentElement.lang).toBe('vi');
    expect(screen.getByTestId('shell-lang-toggle')).toHaveAccessibleName('Chuyển sang English');
  });

  it('toggles the theme and the live state', async () => {
    renderApp({ session: userSession });
    const theme = await screen.findByTestId('shell-theme-toggle');
    const before = document.querySelector('[data-role="user"]')?.getAttribute('data-theme');

    fireEvent.click(theme);
    expect(document.querySelector('[data-role="user"]')?.getAttribute('data-theme')).not.toBe(
      before,
    );

    const live = screen.getByTestId('shell-live-toggle');
    expect(live).toHaveAttribute('aria-pressed', 'true');
    fireEvent.click(live);
    expect(live).toHaveAttribute('aria-pressed', 'false');
    expect(live).toHaveTextContent('Paused');
    expect(await screen.findByTestId('shell-toast')).toHaveTextContent('Live data paused');
  });

  it('hides the counters while nothing produces notifications, downloads or a tray', async () => {
    renderApp({ session: userSession });

    await screen.findByTestId('shell-tray-button');
    expect(screen.queryByTestId('shell-tray-count')).toBeNull();
    expect(screen.queryByTestId('shell-notifications-count')).toBeNull();
    expect(screen.queryByTestId('shell-nav-downloads-count')).toBeNull();
  });

  it('keeps the shell working when the key list fails', async () => {
    server.use(http.get('*/keys', () => new HttpResponse(null, { status: 500 })));
    renderApp({ session: userSession });

    await screen.findByTestId('shell-tray-button');
    expect(screen.getByTestId('shell-nav')).toBeInTheDocument();
    expect(screen.queryByTestId('shell-tray-count')).toBeNull();
  });

  it('survives an HTML answer (the SPA index) where JSON is expected', async () => {
    server.use(
      http.get('*/keys', () => new HttpResponse('<html></html>', { status: 200 })),
      http.get('*/flickr-accounts', () => new HttpResponse('<html></html>', { status: 200 })),
    );
    renderApp({ session: userSession });

    expect(await screen.findByTestId('shell-nav')).toBeInTheDocument();
    expect(screen.queryByTestId('shell-crawling-off-banner')).toBeNull();
  });

  it('submits the search box to /search?q= and ignores empty text', async () => {
    const { router } = renderApp({ session: userSession });
    const input = await screen.findByTestId('shell-search-input');

    fireEvent.submit(screen.getByTestId('shell-search'));
    expect(router.state.location.pathname).toBe('/');
    expect(input).toHaveFocus();

    fireEvent.change(input, { target: { value: 'trees & dog' } });
    fireEvent.submit(screen.getByTestId('shell-search'));
    await waitFor(() => {
      expect(router.state.location.pathname).toBe('/search');
    });
    expect(router.state.location.search).toBe('?q=trees%20%26%20dog');
  });

  it('supports the keyboard shortcuts', async () => {
    const { router } = renderApp({ session: userSession, path: '/library' });
    const input = await screen.findByTestId('shell-search-input');

    fireEvent.keyDown(document.body, { key: '/' });
    expect(input).toHaveFocus();

    fireEvent.keyDown(document.body, { key: 'l' });
    expect(screen.getByTestId('shell-live-toggle')).toHaveAttribute('aria-pressed', 'false');

    fireEvent.keyDown(document.body, { key: 'g' });
    fireEvent.keyDown(document.body, { key: 'h' });
    await waitFor(() => {
      expect(router.state.location.pathname).toBe('/');
    });

    fireEvent.keyDown(document.body, { key: '?' });
    const dialog = await screen.findByTestId('shell-shortcuts');
    fireEvent.keyDown(document, { key: 'Escape' });
    await waitFor(() => {
      expect(dialog).not.toBeInTheDocument();
    });
  });

  it('ignores shortcuts typed in a field', async () => {
    renderApp({ session: userSession });
    const input = await screen.findByTestId('shell-search-input');

    fireEvent.keyDown(input, { key: 'l' });

    expect(screen.getByTestId('shell-live-toggle')).toHaveAttribute('aria-pressed', 'true');
  });
});

describe('user shell global states', () => {
  it('shows disabled tabs and the loading line while /me is pending', () => {
    renderApp({ session: { status: 'loading' } });

    expect(screen.getByText('Loading XFlickr…')).toBeInTheDocument();
    expect(screen.getByTestId('shell-nav-home')).toHaveAttribute('aria-disabled', 'true');
    expect(screen.queryAllByRole('link', { name: 'Library' })).toHaveLength(0);
    expect(screen.queryByTestId('shell-tray-button')).toBeNull();
  });

  it('shows the error banner with Retry when /me failed', () => {
    const retry = vi.fn();
    renderApp({ session: { status: 'error' }, retry });

    expect(screen.getByRole('alert')).toHaveTextContent('Could not load your workspace.');
    fireEvent.click(screen.getByTestId('shell-error-retry'));
    expect(retry).toHaveBeenCalledOnce();
  });

  it('shows the session-expired modal and signs in with return_to', () => {
    const assign = vi.fn();
    vi.stubGlobal('location', { ...window.location, assign, pathname: '/library', search: '' });
    renderApp({ session: { status: 'unauthenticated' }, path: '/library?x=1' });

    const modal = screen.getByTestId('shell-session-modal');
    expect(modal).toHaveAttribute('role', 'alertdialog');
    fireEvent.click(screen.getByTestId('shell-session-sign-in'));
    expect(assign).toHaveBeenCalledWith('/login?reason=expired&return_to=%2Flibrary%3Fx%3D1');
    vi.unstubAllGlobals();
  });

  it('shows the offline banner and announces it, then toasts on return', async () => {
    renderApp({ session: userSession });
    await screen.findByTestId('shell-nav-home');

    fireEvent(window, new Event('offline'));
    expect(await screen.findByTestId('shell-offline-banner')).toHaveTextContent('You are offline');
    expect(screen.getByTestId('shell-live-toggle')).toHaveTextContent('Paused');

    fireEvent(window, new Event('online'));
    await waitFor(() => {
      expect(screen.queryByTestId('shell-offline-banner')).toBeNull();
    });
    expect(screen.getAllByText('Back online').length).toBeGreaterThan(0);
  });

  it('shows key-revoked and token-expired banners with dismiss and a "more" link', async () => {
    server.use(
      http.get('*/keys', () =>
        HttpResponse.json([{ id: 1, label: 'Main key', status: 'revoked' }]),
      ),
      http.get('*/flickr-accounts', () =>
        HttpResponse.json([
          { id: 1, key_id: 11, username: 'anna', status: 'reconnect_required' },
          { id: 2, key_id: 12, username: 'bob', status: 'reconnect_required' },
          { id: 3, key_id: 13, username: 'cy', status: 'reconnect_required' },
          { id: 4, key_id: 14, username: 'dee', status: 'ok' },
        ]),
      ),
    );
    renderApp({ session: userSession });

    const key = await screen.findByTestId('shell-key-revoked-banner-1');
    expect(key).toHaveTextContent('Main key');
    expect(screen.getByTestId('shell-token-expired-banner-11')).toBeInTheDocument();
    expect(screen.getByTestId('shell-token-expired-banner-12')).toBeInTheDocument();
    expect(screen.queryByTestId('shell-token-expired-banner-13')).toBeNull();
    expect(screen.queryByTestId('shell-token-expired-banner-14')).toBeNull();
    expect(screen.getByRole('link', { name: '1 more' })).toHaveAttribute('href', '/accounts');

    fireEvent.click(screen.getByTestId('shell-banner-dismiss-1'));
    expect(screen.queryByTestId('shell-key-revoked-banner-1')).toBeNull();
  });

  it('starts the Flickr authorization of the key when reconnecting an account', async () => {
    server.use(
      http.get('*/flickr-accounts', () =>
        HttpResponse.json([{ id: 1, key_id: 11, username: 'anna', status: 'reconnect_required' }]),
      ),
      http.post('*/flickr-accounts/connect', async ({ request }) => {
        expect(await request.json()).toEqual({ key_id: 11 });
        return HttpResponse.json({ authorize_url: '/authorized' });
      }),
    );
    const assign = vi.fn();
    vi.stubGlobal('location', { ...window.location, assign });
    renderApp({ session: userSession });

    fireEvent.click(await screen.findByTestId('shell-banner-reconnect-11'));

    await waitFor(() => {
      expect(assign).toHaveBeenCalledWith('/authorized');
    });
    vi.unstubAllGlobals();
  });

  describe('reconnecting a Flickr account', () => {
    const tokenBanner = () =>
      server.use(
        http.get('*/flickr-accounts', () =>
          HttpResponse.json([
            { id: 1, key_id: 11, username: 'anna', status: 'reconnect_required' },
          ]),
        ),
      );

    it.each([
      [
        'an HTML 200',
        () =>
          new HttpResponse('<!doctype html><html></html>', {
            headers: { 'Content-Type': 'text/html' },
          }),
      ],
      ['a null body', () => HttpResponse.json(null)],
      ['a body without authorize_url', () => HttpResponse.json({})],
      ['a 500', () => new HttpResponse(null, { status: 500 })],
    ])('shows an error instead of crashing on %s', async (_name, respond) => {
      tokenBanner();
      server.use(http.post('*/flickr-accounts/connect', respond));
      renderApp({ session: userSession });

      fireEvent.click(await screen.findByTestId('shell-banner-reconnect-11'));

      expect(await screen.findByTestId('shell-reconnect-error')).toBeInTheDocument();
      expect(screen.getByTestId('shell-nav')).toBeInTheDocument();
    });
  });

  it('shows the maintenance page after three failed health probes', async () => {
    server.use(http.get('*/healthz', () => new HttpResponse(null, { status: 503 })));
    vi.useFakeTimers({ shouldAdvanceTime: true });
    renderApp({ session: userSession });

    await vi.advanceTimersByTimeAsync(40_000);

    expect(await screen.findByTestId('shell-maintenance')).toHaveTextContent(
      'XFlickr is under maintenance',
    );
    expect(screen.queryByTestId('shell-nav')).toBeNull();

    server.use(http.get('*/healthz', () => HttpResponse.json({ status: 'ok', version: 'test' })));
    fireEvent.click(screen.getByTestId('shell-maintenance-retry'));
    await vi.advanceTimersByTimeAsync(100);
    vi.useRealTimers();
    expect(await screen.findByTestId('shell-nav')).toBeInTheDocument();
  });
});

describe('maintenance probe', () => {
  it('treats an HTML 200 from /healthz as a failed probe', async () => {
    server.use(
      http.get(
        '*/healthz',
        () => new HttpResponse('<!doctype html>', { headers: { 'Content-Type': 'text/html' } }),
      ),
    );
    vi.useFakeTimers({ shouldAdvanceTime: true });
    renderApp({ session: userSession });

    await vi.advanceTimersByTimeAsync(40_000);
    vi.useRealTimers();

    expect(await screen.findByTestId('shell-maintenance')).toBeInTheDocument();
  });
});

describe('user shell access to /admin', () => {
  it('shows the 403 page and no admin chrome for the user role', async () => {
    renderApp({ session: userSession, path: '/admin/users' });

    expect(await screen.findByTestId('admin-forbidden')).toHaveTextContent(
      'You do not have access to this page',
    );
    expect(screen.queryByTestId('admin-rail')).toBeNull();
    expect(screen.queryByTestId('admin-nav-users')).toBeNull();
    fireEvent.click(screen.getByTestId('admin-forbidden-cta'));
    expect(await screen.findByTestId('shell-page')).toBeInTheDocument();
  });

  it('lands an admin on /admin from / and remembers the User switch', async () => {
    const { router } = renderApp({ session: adminSession, path: '/' });

    expect(await screen.findByTestId('admin-rail')).toBeInTheDocument();
    expect(router.state.location.pathname).toBe('/admin');

    fireEvent.click(
      within(screen.getByTestId('admin-role-switch')).getByRole('button', { name: /user/i }),
    );
    expect(await screen.findByTestId('shell-page')).toBeInTheDocument();
    expect(router.state.location.pathname).toBe('/');
    expect(window.localStorage.getItem('xf_role_view')).toBe('user');
  });

  it('keeps an admin on / when the stored choice is User', async () => {
    window.localStorage.setItem('xf_role_view', 'user');
    const { router } = renderApp({ session: adminSession, path: '/' });

    expect(await screen.findByTestId('shell-page')).toBeInTheDocument();
    expect(router.state.location.pathname).toBe('/');
  });

  it.each(['/library?tab=2', '/crawl', '/profile'])(
    'opens the deep link %s for an admin without rewriting it',
    async (path) => {
      const { router } = renderApp({ session: adminSession, path });

      expect(await screen.findByTestId('shell-nav')).toBeInTheDocument();
      const { pathname, search } = router.state.location;
      expect(pathname + search).toBe(path);
    },
  );

  it('opens an admin screen directly for an admin and remembers Admin', async () => {
    window.localStorage.setItem('xf_role_view', 'user');
    const { router } = renderApp({ session: adminSession, path: '/admin/users?page=2' });

    expect(await screen.findByTestId('admin-rail')).toBeInTheDocument();
    expect(router.state.location.pathname + router.state.location.search).toBe(
      '/admin/users?page=2',
    );
    expect(window.localStorage.getItem('xf_role_view')).toBe('admin');
  });

  it('still lands on /admin when storage is unavailable', async () => {
    const spy = vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('denied');
    });
    const { router } = renderApp({ session: adminSession, path: '/' });

    expect(await screen.findByTestId('admin-rail')).toBeInTheDocument();
    expect(router.state.location.pathname).toBe('/admin');
    spy.mockRestore();
  });
});

describe('crawling-off banner', () => {
  const validKey = apiKeyBody();

  it.each(['/', '/library', '/profile', '/keys', '/onboarding'])(
    'shows the banner on %s while no valid key exists and links to the wizard',
    async (path) => {
      renderApp({ session: userSession, path });

      const banner = await screen.findByTestId('shell-crawling-off-banner');
      expect(banner).toHaveTextContent('Crawling is off');
      expect(screen.getByTestId('shell-banner-add-key')).toHaveAttribute('href', '/onboarding');
    },
  );

  it('stays hidden for a valid key and counts a revoked-only list as no valid key', async () => {
    server.use(http.get('*/keys', () => HttpResponse.json([validKey])));
    const first = renderApp({ session: userSession, path: '/profile' });
    await screen.findByTestId('shell-nav-home');
    expect(screen.queryByTestId('shell-crawling-off-banner')).toBeNull();
    first.unmount();

    server.use(http.get('*/keys', () => HttpResponse.json([{ ...validKey, status: 'revoked' }])));
    renderApp({ session: userSession, path: '/profile' });
    expect(await screen.findByTestId('shell-crawling-off-banner')).toBeInTheDocument();
  });

  it('stays hidden while the key list fails', async () => {
    server.use(http.get('*/keys', () => new HttpResponse(null, { status: 500 })));
    renderApp({ session: userSession, path: '/profile' });
    await screen.findByTestId('shell-nav-home');
    expect(screen.queryByTestId('shell-crawling-off-banner')).toBeNull();
  });

  it('keeps the offline banner first and the token cap of 2 with the banner shown', async () => {
    server.use(
      http.get('*/keys', () =>
        HttpResponse.json([{ id: 1, label: 'Main key', status: 'revoked' }]),
      ),
      http.get('*/flickr-accounts', () =>
        HttpResponse.json([
          { id: 1, key_id: 11, username: 'anna', status: 'reconnect_required' },
          { id: 2, key_id: 12, username: 'bob', status: 'reconnect_required' },
          { id: 3, key_id: 13, username: 'cy', status: 'reconnect_required' },
        ]),
      ),
    );
    renderApp({ session: userSession, path: '/profile' });
    await screen.findByTestId('shell-crawling-off-banner');
    fireEvent(window, new Event('offline'));
    await screen.findByTestId('shell-offline-banner');
    await screen.findByTestId('shell-key-revoked-banner-1');

    const order = Array.from(
      document.querySelectorAll(
        '[data-testid="shell-offline-banner"],[data-testid="shell-crawling-off-banner"],[data-testid^="shell-key-revoked-banner"],[data-testid^="shell-token-expired-banner"]',
      ),
    ).map((node) => node.getAttribute('data-testid'));
    expect(order).toEqual([
      'shell-offline-banner',
      'shell-crawling-off-banner',
      'shell-key-revoked-banner-1',
      'shell-token-expired-banner-11',
      'shell-token-expired-banner-12',
    ]);
  });
});
