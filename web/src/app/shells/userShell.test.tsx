import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw/http';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { server } from '../../test/server';
import { adminSession, renderApp, userSession } from '../../test/render';
import { resetBootHandled } from '../guards';
import { USER_NAV } from './nav';

beforeEach(resetBootHandled);

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
    'routes $id to a placeholder page and marks the tab current',
    async (item) => {
      renderApp({ session: userSession, path: item.path });

      expect(await screen.findByRole('heading', { level: 1 })).toBeInTheDocument();
      expect(screen.getByTestId(`shell-nav-${item.id}`)).toHaveAttribute('aria-current', 'page');
      if (item.id === 'profile') {
        expect(screen.getByTestId('profile-page')).toBeInTheDocument();
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

  it('shows summary badges, hiding the Downloads count at 0', async () => {
    renderApp({ session: userSession });

    expect(await screen.findByTestId('shell-tray-count')).toHaveTextContent('12');
    expect(screen.getByTestId('shell-notifications-count')).toHaveTextContent('3');
    expect(screen.getByTestId('shell-nav-downloads-count')).toHaveTextContent('2');
    expect(screen.getByTestId('shell-notifications-button')).toHaveAccessibleName(
      'Notifications, 3 unread',
    );
  });

  it('hides badges when the summary fails', async () => {
    server.use(http.get('*/shell/summary', () => new HttpResponse(null, { status: 500 })));
    renderApp({ session: userSession });

    await screen.findByTestId('shell-tray-button');
    await waitFor(() => {
      expect(screen.queryByTestId('shell-tray-count')).toBeNull();
    });
    expect(screen.queryByTestId('shell-notifications-count')).toBeNull();
  });

  it('shows 99+ above 99 unread', async () => {
    server.use(
      http.get('*/shell/summary', () =>
        HttpResponse.json({
          unread_notifications: 120,
          running_downloads: 0,
          tray_count: 0,
          attention: [],
        }),
      ),
    );
    renderApp({ session: userSession });

    expect(await screen.findByTestId('shell-notifications-count')).toHaveTextContent('99+');
    expect(screen.queryByTestId('shell-nav-downloads-count')).toBeNull();
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
      http.get('*/shell/summary', () =>
        HttpResponse.json({
          unread_notifications: 0,
          running_downloads: 0,
          tray_count: 0,
          attention: [
            { kind: 'key_revoked', id: 'k1', label: 'Main key' },
            { kind: 'token_expired', id: 'a1', label: 'anna' },
            { kind: 'token_expired', id: 'a2', label: 'bob' },
            { kind: 'cloud_expired', id: 'c1', label: 'Drive' },
          ],
        }),
      ),
    );
    renderApp({ session: userSession });

    const key = await screen.findByTestId('shell-key-revoked-banner-k1');
    expect(key).toHaveTextContent('Main key');
    expect(screen.getByTestId('shell-token-expired-banner-a1')).toBeInTheDocument();
    expect(screen.getByTestId('shell-token-expired-banner-a2')).toBeInTheDocument();
    expect(screen.queryByTestId('shell-token-expired-banner-c1')).toBeNull();
    expect(screen.getByRole('link', { name: '1 more' })).toHaveAttribute('href', '/accounts');

    fireEvent.click(screen.getByTestId('shell-banner-dismiss-k1'));
    expect(screen.queryByTestId('shell-key-revoked-banner-k1')).toBeNull();
  });

  it('reconnects a cloud banner through /sync', async () => {
    server.use(
      http.get('*/shell/summary', () =>
        HttpResponse.json({
          unread_notifications: 0,
          running_downloads: 0,
          tray_count: 0,
          attention: [{ kind: 'cloud_expired', id: 'c1', label: 'Drive' }],
        }),
      ),
    );
    const { router } = renderApp({ session: userSession });

    fireEvent.click(await screen.findByTestId('shell-banner-reconnect-c1'));

    await waitFor(() => {
      expect(router.state.location.pathname).toBe('/sync');
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

    server.use(http.get('*/healthz', () => HttpResponse.json({ status: 'ok' })));
    fireEvent.click(screen.getByTestId('shell-maintenance-retry'));
    await vi.advanceTimersByTimeAsync(100);
    vi.useRealTimers();
    expect(await screen.findByTestId('shell-nav')).toBeInTheDocument();
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

  it('redirects an admin who lands on a user route to /admin once', async () => {
    const { router } = renderApp({ session: adminSession, path: '/library' });

    expect(await screen.findByTestId('admin-rail')).toBeInTheDocument();
    expect(router.state.location.pathname).toBe('/admin');

    fireEvent.click(
      within(screen.getByTestId('admin-role-switch')).getByRole('button', { name: /user/i }),
    );
    expect(await screen.findByTestId('shell-page')).toBeInTheDocument();
    expect(router.state.location.pathname).toBe('/');
  });
});

describe('crawling-off banner', () => {
  const validKey = { id: 1, label: 'Main', status: 'valid' };

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
      http.get('*/shell/summary', () =>
        HttpResponse.json({
          unread_notifications: 0,
          running_downloads: 0,
          tray_count: 0,
          attention: [
            { kind: 'key_revoked', id: 'k1', label: 'Main key' },
            { kind: 'token_expired', id: 'a1', label: 'anna' },
            { kind: 'token_expired', id: 'a2', label: 'bob' },
            { kind: 'token_expired', id: 'a3', label: 'cy' },
          ],
        }),
      ),
    );
    renderApp({ session: userSession, path: '/profile' });
    await screen.findByTestId('shell-crawling-off-banner');
    fireEvent(window, new Event('offline'));
    await screen.findByTestId('shell-offline-banner');
    await screen.findByTestId('shell-key-revoked-banner-k1');

    const order = Array.from(
      document.querySelectorAll(
        '[data-testid="shell-offline-banner"],[data-testid="shell-crawling-off-banner"],[data-testid^="shell-key-revoked-banner"],[data-testid^="shell-token-expired-banner"]',
      ),
    ).map((node) => node.getAttribute('data-testid'));
    expect(order).toEqual([
      'shell-offline-banner',
      'shell-crawling-off-banner',
      'shell-key-revoked-banner-k1',
      'shell-token-expired-banner-a1',
      'shell-token-expired-banner-a2',
    ]);
  });
});
