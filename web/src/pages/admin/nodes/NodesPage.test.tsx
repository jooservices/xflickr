import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { onlineManager } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw/http';
import { describe, expect, it, vi } from 'vitest';
import type { AdminKeyQuota } from '../../../shared/api/gen/models';
import { adminSession, renderApp, userSession } from '../../../test/render';
import { server } from '../../../test/server';

const key = (overrides: Partial<AdminKeyQuota> = {}): AdminKeyQuota => ({
  key_id: 42,
  user_id: 7,
  label: 'Photo archive',
  node: 'reporting-node-1',
  used: 900,
  limit: 3600,
  state: 'cooling',
  trend: 'rising',
  ...overrides,
});

describe('cluster nodes page', () => {
  it.each([
    {
      locale: 'en' as const,
      subtitle: 'Key labels, IDs, users, and current quota states.',
    },
    {
      locale: 'vi' as const,
      subtitle: 'Nhãn khóa, mã khóa, người dùng và trạng thái quota hiện tại.',
    },
  ])('describes the displayed quota data in $locale', async ({ locale, subtitle }) => {
    server.use(http.get('*/admin/quota', () => HttpResponse.json({ keys: [] })));
    renderApp({ session: adminSession, path: '/admin/nodes', locale });

    expect(await screen.findByText(subtitle)).toBeInTheDocument();
  });

  it('renders key quota states without exposing secrets or reporting node as placement', async () => {
    server.use(
      http.get('*/admin/quota', () =>
        HttpResponse.json({
          keys: [
            key({ state: 'ok' }),
            key({ key_id: 43, label: 'Photo sync', state: 'cooling' }),
            key({ key_id: 44, label: 'Photo import', user_id: undefined, state: 'exhausted' }),
          ].map((item) => ({ ...item, key_secret: 'never-render-this-secret' })),
        }),
      ),
    );
    renderApp({ session: adminSession, path: '/admin/nodes' });

    const panel = await screen.findByTestId('admin-nodes-key-placement');
    expect(screen.getByRole('heading', { level: 1, name: 'Cluster nodes' })).toBeInTheDocument();
    expect(within(panel).getByRole('columnheader', { name: 'Key' })).toBeInTheDocument();
    expect(within(panel).getByRole('columnheader', { name: 'User' })).toBeInTheDocument();
    expect(within(panel).getByRole('columnheader', { name: 'State' })).toBeInTheDocument();
    expect(within(panel).getByText('Photo archive')).toBeInTheDocument();
    expect(within(panel).getByText('#42')).toBeInTheDocument();
    expect(within(panel).getAllByText('User #7')).toHaveLength(2);
    expect(within(panel).getByText('Photo sync')).toBeInTheDocument();
    expect(within(panel).getByText('OK')).toBeInTheDocument();
    expect(within(panel).getByText('Cooling')).toBeInTheDocument();
    expect(within(panel).getByText('Exhausted')).toBeInTheDocument();
    expect(within(panel).getByText('—')).toBeInTheDocument();
    expect(within(panel).queryByText('reporting-node-1')).not.toBeInTheDocument();
    expect(within(panel).queryByText('never-render-this-secret')).not.toBeInTheDocument();
    expect(within(panel).queryByRole('columnheader', { name: 'Node' })).not.toBeInTheDocument();
  });

  it('shows loading, empty, and error states and retries successfully', async () => {
    let release: (() => void) | undefined;
    const pending = new Promise<void>((resolve) => {
      release = resolve;
    });
    let calls = 0;
    server.use(
      http.get('*/admin/quota', async () => {
        calls += 1;
        if (calls === 1) {
          await pending;
          return new HttpResponse(null, { status: 503 });
        }
        return HttpResponse.json({ keys: [] });
      }),
    );
    renderApp({ session: adminSession, path: '/admin/nodes' });

    expect(await screen.findByTestId('admin-nodes-loading')).toHaveTextContent(
      'Loading key states…',
    );
    release?.();
    const error = await screen.findByTestId('admin-nodes-error');
    expect(error).toHaveTextContent('Could not load key states.');
    fireEvent.click(within(error).getByTestId('admin-nodes-error-retry'));

    expect(await screen.findByTestId('admin-nodes-empty')).toHaveTextContent('No active keys');
    expect(calls).toBe(2);
    expect(
      within(screen.getByTestId('admin-nodes-page')).queryByRole('link'),
    ).not.toBeInTheDocument();
  });

  it('retains stale data while refreshing and after a refresh failure', async () => {
    let calls = 0;
    server.use(
      http.get('*/admin/quota', () => {
        calls += 1;
        return calls === 1
          ? HttpResponse.json({ keys: [key({ label: 'Retained key' })] })
          : calls === 2
            ? new HttpResponse(null, { status: 500 })
            : HttpResponse.json({ keys: [key({ label: 'Updated key' })] });
      }),
    );
    renderApp({ session: adminSession, path: '/admin/nodes' });

    const panel = await screen.findByTestId('admin-nodes-key-placement');
    const refresh = screen.getByTestId('admin-nodes-refresh');
    fireEvent.click(refresh);
    await waitFor(() => expect(calls).toBe(2));
    expect(within(panel).getByText('Retained key')).toBeInTheDocument();

    expect(await screen.findByTestId('admin-nodes-stale')).toHaveTextContent(
      'Data may be out of date',
    );
    expect(within(panel).getByText('Retained key')).toBeInTheDocument();
    fireEvent.click(screen.getByTestId('admin-nodes-error-retry'));
    expect(await within(panel).findByText('Updated key')).toBeInTheDocument();
  });

  it('protects quota data for users and replaces a forbidden response', async () => {
    let calls = 0;
    server.use(
      http.get('*/admin/quota', () => {
        calls += 1;
        return calls === 1
          ? HttpResponse.json({ keys: [key({ label: 'Private quota row' })] })
          : new HttpResponse(null, { status: 403 });
      }),
    );
    const user = renderApp({ session: userSession, path: '/admin/nodes' });

    expect(screen.getByTestId('admin-forbidden')).toBeInTheDocument();
    expect(calls).toBe(0);
    user.unmount();

    const admin = renderApp({ session: adminSession, path: '/admin/nodes' });
    await waitFor(() => expect(calls).toBe(1));
    expect(await screen.findByText('Private quota row')).toBeInTheDocument();
    const refresh = screen.getByTestId('admin-nodes-refresh');
    await waitFor(() => expect(refresh).toBeEnabled());
    fireEvent.click(refresh);

    const forbidden = await screen.findByTestId('admin-nodes-forbidden');
    expect(forbidden).toBeInTheDocument();
    expect(screen.queryByTestId('admin-nodes-key-placement')).not.toBeInTheDocument();
    expect(screen.queryByText('Private quota row')).not.toBeInTheDocument();
    fireEvent.click(screen.getByTestId('admin-nodes-forbidden-home'));
    await waitFor(() => expect(admin.router.state.location.pathname).not.toBe('/admin/nodes'));
  });

  it('offers a one-time load when the shared query is paused', async () => {
    let calls = 0;
    onlineManager.setOnline(false);
    server.use(
      http.get('*/admin/quota', () => {
        calls += 1;
        return HttpResponse.json({ keys: [key({ label: 'One-time snapshot' })] });
      }),
    );
    const app = renderApp({ session: adminSession, path: '/admin/nodes' });

    try {
      expect(await screen.findByText('No API quota snapshot yet.')).toBeInTheDocument();
      expect(calls).toBe(0);
      fireEvent.click(screen.getByRole('button', { name: 'Load once' }));
      expect(await screen.findByText('One-time snapshot')).toBeInTheDocument();
      expect(calls).toBe(1);
    } finally {
      app.unmount();
      onlineManager.setOnline(true);
    }
  });

  it('pauses quota polling when Live is off and fetches again when it resumes', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      let calls = 0;
      server.use(
        http.get('*/admin/quota', () => {
          calls += 1;
          return HttpResponse.json({
            keys: [key({ label: calls === 1 ? 'First snapshot' : 'Resumed snapshot' })],
          });
        }),
      );
      renderApp({ session: adminSession, path: '/admin/nodes' });
      await screen.findByText('First snapshot');

      const liveToggle = screen.getByTestId('admin-live-toggle');
      fireEvent.click(liveToggle);
      await vi.advanceTimersByTimeAsync(20_000);
      expect(calls).toBe(1);

      fireEvent.click(liveToggle);
      expect(await screen.findByText('Resumed snapshot')).toBeInTheDocument();
      expect(calls).toBe(2);
    } finally {
      vi.useRealTimers();
      onlineManager.setOnline(true);
    }
  });
});
