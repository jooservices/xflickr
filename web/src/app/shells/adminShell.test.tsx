import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw/http';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { adminShellFixture } from '../../test/handlers';
import { adminSession, renderApp, userSession } from '../../test/render';
import { server } from '../../test/server';
import { resetBootHandled } from '../guards';
import { ADMIN_NAV, ADMIN_NAV_GROUPS } from './nav';

beforeEach(resetBootHandled);

describe('admin shell', () => {
  it('renders the 14 rail items in three groups, in order', async () => {
    renderApp({ session: adminSession, path: '/admin' });

    const rail = await screen.findByTestId('admin-rail');
    expect(rail).toHaveAccessibleName('Admin sections');
    expect(within(rail).getAllByRole('group')).toHaveLength(ADMIN_NAV_GROUPS.length);
    const links = within(rail)
      .getAllByRole('link')
      .filter((link) => link.dataset.testid?.startsWith('admin-nav-'));
    expect(links.map((link) => link.getAttribute('href'))).toEqual(
      ADMIN_NAV.map((item) => item.path),
    );
    expect(ADMIN_NAV).toHaveLength(14);
    expect(screen.getByTestId('admin-nav-overview')).toHaveAttribute('aria-current', 'page');
    expect(screen.getByTestId('admin-shell')).toHaveAttribute('data-role', 'admin');
    expect(screen.getByTestId('admin-shell')).toHaveAttribute('data-theme', 'dark');
  });

  it.each(ADMIN_NAV)('routes $id to a placeholder page and highlights it', async (item) => {
    renderApp({ session: adminSession, path: item.path });

    expect(await screen.findByTestId('admin-page')).toBeInTheDocument();
    expect(screen.getByTestId(`admin-nav-${item.id}`)).toHaveAttribute('aria-current', 'page');
    expect(screen.getByRole('heading', { level: 1 })).toBeInTheDocument();
  });

  it('highlights Cluster nodes on a node detail route', async () => {
    renderApp({ session: adminSession, path: '/admin/nodes/n1' });

    expect(await screen.findByTestId('admin-nav-nodes')).toHaveAttribute('aria-current', 'page');
  });

  it('shows the 404 page for an unknown admin route', async () => {
    renderApp({ session: adminSession, path: '/admin/nope' });

    expect(await screen.findByTestId('shell-notfound')).toBeInTheDocument();
  });

  it('shows the Issues badge in the link name and the bell count', async () => {
    renderApp({ session: adminSession, path: '/admin' });

    expect(await screen.findByTestId('admin-nav-issues-badge')).toHaveTextContent('3');
    expect(screen.getByTestId('admin-nav-issues')).toHaveAccessibleName('Issues, 3 open');
    expect(screen.getByTestId('admin-bell-count')).toHaveTextContent('2');
  });

  it('hides the badge and count at 0', async () => {
    server.use(
      http.get('*/admin/shell', () =>
        HttpResponse.json({ ...adminShellFixture, open_issues: 0, unread_notifications: 0 }),
      ),
    );
    renderApp({ session: adminSession, path: '/admin' });

    await screen.findByTestId('admin-cluster-status');
    expect(screen.queryByTestId('admin-nav-issues-badge')).toBeNull();
    expect(screen.queryByTestId('admin-bell-count')).toBeNull();
  });

  it('shows the cluster footer with status, summary and sparkline', async () => {
    renderApp({ session: adminSession, path: '/admin' });

    expect(screen.getByTestId('admin-cluster-loading')).toBeInTheDocument();
    expect(await screen.findByTestId('admin-cluster-status')).toHaveTextContent('Healthy');
    expect(screen.getByTestId('admin-cluster-summary')).toHaveTextContent('12 calls/s · 4/4 nodes');
    expect(screen.getByTestId('admin-cluster-spark')).toHaveAccessibleName(
      'Cluster calls per second, last 60 minutes, now 12',
    );
    expect(screen.getByTestId('admin-cluster-footer')).toHaveAttribute('href', '/admin/nodes');
    expect(screen.queryByTestId('admin-cluster-stale')).toBeNull();
  });

  it.each([
    ['degraded', 'Degraded'],
    ['down', 'Down'],
  ])('shows the %s cluster status as a word', async (status, word) => {
    server.use(
      http.get('*/admin/shell', () =>
        HttpResponse.json({
          ...adminShellFixture,
          cluster: { ...adminShellFixture.cluster, status },
        }),
      ),
    );
    renderApp({ session: adminSession, path: '/admin' });

    expect(await screen.findByTestId('admin-cluster-status')).toHaveTextContent(word);
  });

  it('shows the empty footer when no node joined', async () => {
    server.use(
      http.get('*/admin/shell', () =>
        HttpResponse.json({
          ...adminShellFixture,
          cluster: {
            status: 'unknown',
            nodes_total: 0,
            nodes_healthy: 0,
            calls_per_second: 0,
            calls_history: [],
          },
        }),
      ),
    );
    renderApp({ session: adminSession, path: '/admin' });

    expect(await screen.findByTestId('admin-cluster-empty')).toHaveTextContent('No nodes joined');
    expect(screen.getByRole('link', { name: 'Add a node' })).toHaveAttribute(
      'href',
      '/admin/nodes',
    );
    expect(screen.queryByTestId('admin-cluster-spark')).toBeNull();
  });

  it('shows the footer error with Retry, without hiding the content', async () => {
    let calls = 0;
    server.use(
      http.get('*/admin/shell', () => {
        calls += 1;
        return calls === 1
          ? new HttpResponse(null, { status: 500 })
          : HttpResponse.json(adminShellFixture);
      }),
    );
    renderApp({ session: adminSession, path: '/admin' });

    expect(await screen.findByTestId('admin-cluster-error')).toHaveTextContent(
      'Cluster status unavailable',
    );
    expect(screen.getByTestId('admin-page')).toBeInTheDocument();
    expect(screen.queryByTestId('admin-nav-issues-badge')).toBeNull();

    fireEvent.click(screen.getByTestId('admin-cluster-retry'));
    expect(await screen.findByTestId('admin-cluster-status')).toHaveTextContent('Healthy');
  });

  it('marks the footer stale when live is paused', async () => {
    renderApp({ session: adminSession, path: '/admin' });
    await screen.findByTestId('admin-cluster-status');

    fireEvent.click(screen.getByTestId('admin-live-toggle'));

    expect(await screen.findByTestId('admin-cluster-stale')).toHaveTextContent('Stale');
    expect(screen.getByTestId('admin-live-toggle')).toHaveTextContent('Paused');
  });

  it('switches language and theme from the top bar', async () => {
    renderApp({ session: adminSession, path: '/admin' });
    await screen.findByTestId('admin-rail');

    fireEvent.click(screen.getByTestId('admin-lang-toggle'));
    expect(await screen.findByTestId('admin-nav-nodes')).toHaveTextContent('Các node');
    expect(screen.getByRole('navigation', { name: 'Các mục quản trị' })).toBeInTheDocument();

    fireEvent.click(screen.getByTestId('admin-theme-toggle'));
    expect(screen.getByTestId('admin-shell')).toHaveAttribute('data-theme', 'light');
  });

  it('shows the role switch only to admins and routes the User button to /', async () => {
    const { router } = renderApp({ session: adminSession, path: '/admin' });
    const group = await screen.findByTestId('admin-role-switch');
    expect(within(group).getByRole('button', { name: 'Admin' })).toHaveAttribute(
      'aria-pressed',
      'true',
    );

    fireEvent.click(within(group).getByRole('button', { name: 'User' }));

    await waitFor(() => {
      expect(router.state.location.pathname).toBe('/');
    });
  });

  it('shows the loading spinner and no rail while the role is unknown', () => {
    renderApp({ session: { status: 'loading' }, path: '/admin' });

    expect(screen.getByText('Checking your access…')).toBeInTheDocument();
    expect(screen.getByTestId('admin-topbar')).toBeInTheDocument();
    expect(screen.queryByTestId('admin-rail')).toBeNull();
    expect(screen.queryByTestId('admin-role-switch')).toBeNull();
  });

  it('shows the /me failure banner with Retry and no rail', () => {
    const retry = vi.fn();
    renderApp({ session: { status: 'error' }, path: '/admin', retry });

    expect(screen.getByRole('alert')).toHaveTextContent('Could not load the admin area.');
    expect(screen.queryByTestId('admin-rail')).toBeNull();
    fireEvent.click(screen.getByTestId('admin-error-retry'));
    expect(retry).toHaveBeenCalledOnce();
  });

  it('never fetches admin data for a non-admin', async () => {
    const hit = vi.fn();
    server.use(
      http.get('*/admin/shell', () => {
        hit();
        return HttpResponse.json(adminShellFixture);
      }),
    );
    renderApp({ session: userSession, path: '/admin' });

    expect(await screen.findByTestId('admin-forbidden')).toBeInTheDocument();
    expect(hit).not.toHaveBeenCalled();
  });
});

describe('admin shell on a narrow screen', () => {
  function stubNarrow() {
    vi.stubGlobal('matchMedia', (query: string) => ({
      matches: query.includes('900'),
      addEventListener: () => undefined,
      removeEventListener: () => undefined,
    }));
  }

  it('opens the rail as a modal drawer and closes it with Esc, the scrim and an item', async () => {
    stubNarrow();
    const { router } = renderApp({ session: adminSession, path: '/admin' });
    const menu = await screen.findByTestId('admin-menu-btn');
    expect(menu).toHaveAttribute('aria-expanded', 'false');

    const focus = vi.spyOn(menu, 'focus');
    fireEvent.click(menu);
    const rail = screen.getByTestId('admin-rail');
    expect(rail).toHaveAttribute('role', 'dialog');
    expect(rail).toHaveAttribute('aria-modal', 'true');
    expect(screen.getByTestId('admin-main')).toHaveAttribute('inert');
    expect(screen.getByTestId('admin-rail-scrim')).toBeInTheDocument();

    fireEvent.keyDown(document, { key: 'Escape' });
    await waitFor(() => {
      expect(screen.queryByTestId('admin-rail-scrim')).toBeNull();
    });
    // jsdom does not apply the media query that shows the menu button, so assert the call.
    await waitFor(() => {
      expect(focus).toHaveBeenCalled();
    });
    expect(screen.getByTestId('admin-rail')).not.toHaveAttribute('role');

    fireEvent.click(menu);
    fireEvent.click(screen.getByTestId('admin-rail-scrim'));
    expect(screen.queryByTestId('admin-rail-scrim')).toBeNull();

    fireEvent.click(menu);
    fireEvent.click(screen.getByTestId('admin-nav-quota'));
    await waitFor(() => {
      expect(router.state.location.pathname).toBe('/admin/quota');
    });
    expect(screen.queryByTestId('admin-rail-scrim')).toBeNull();
    vi.unstubAllGlobals();
  });
});
