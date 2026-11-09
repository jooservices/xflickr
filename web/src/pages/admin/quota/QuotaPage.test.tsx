import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw/http';
import { describe, expect, it } from 'vitest';
import { loadAdminQuota } from '../../../features/admin/quota';
import { adminSession, renderApp } from '../../../test/render';
import { server } from '../../../test/server';

const quota = (keys: unknown[], status = 200) =>
  http.get('*/admin/quota', () => HttpResponse.json({ keys }, { status }));

const key = {
  key_id: 42,
  user_id: 7,
  label: 'AB12CD••••••••',
  node: 'crawl-1',
  used: 900,
  limit: 3600,
  reset_at: new Date(Date.now() + 60_000).toISOString(),
  state: 'cooling',
  trend: 'rising',
};

describe('API quota page', () => {
  it('loads the generated quota operation without a caller signal', async () => {
    server.use(quota([key]));

    await expect(loadAdminQuota()).resolves.toEqual({ keys: [key] });
  });

  it('renders live per-key usage, a bounded progress bar, state and reset', async () => {
    server.use(quota([key]));
    renderApp({ session: adminSession, path: '/admin/quota' });

    const table = await screen.findByTestId('admin-quota-key-table');
    expect(screen.getByRole('heading', { level: 1, name: 'API quota' })).toBeInTheDocument();
    expect(within(table).getByText('AB12CD••••••••')).toBeInTheDocument();
    expect(within(table).getByText('User #7')).toBeInTheDocument();
    expect(within(table).getByText('crawl-1')).toBeInTheDocument();
    expect(within(table).getByText('Cooling')).toBeInTheDocument();
    expect(within(table).getByRole('progressbar', { name: 'Used this hour' })).toHaveAttribute(
      'aria-valuenow',
      '900',
    );
    expect(within(table).getByText('900 / 3,600')).toBeInTheDocument();
    expect(within(table).getByTestId('admin-quota-cooldown')).toHaveTextContent('Resets in');
    expect(
      screen.getByText(
        'Lane totals, burst values and key controls are not included in the available API response.',
      ),
    ).toBeInTheDocument();
    expect(screen.getByTestId('admin-quota-node-table')).toHaveTextContent(
      'Per-node quota data is not included',
    );
  });

  it('announces loading after the shared skeleton delay', async () => {
    let release: (() => void) | undefined;
    const pending = new Promise<void>((resolve) => {
      release = resolve;
    });
    server.use(
      http.get('*/admin/quota', async () => {
        await pending;
        return HttpResponse.json({ keys: [key] });
      }),
    );
    renderApp({ session: adminSession, path: '/admin/quota' });

    expect(await screen.findByTestId('admin-quota-loading')).toHaveTextContent(
      'Loading API quota…',
    );
    release?.();
    expect(await screen.findByTestId('admin-quota-key-table')).toBeInTheDocument();
  });

  it('refreshes the current quota data', async () => {
    let used = 100;
    server.use(
      http.get('*/admin/quota', () =>
        HttpResponse.json({ keys: [{ ...key, used, state: 'ok', reset_at: undefined }] }),
      ),
    );
    renderApp({ session: adminSession, path: '/admin/quota' });

    const table = await screen.findByTestId('admin-quota-key-table');
    expect(within(table).getByText('100 / 3,600')).toBeInTheDocument();
    used = 250;
    fireEvent.click(screen.getByTestId('admin-quota-refresh'));

    expect(await within(table).findByText('250 / 3,600')).toBeInTheDocument();
  });

  it('shows exhausted state as text', async () => {
    server.use(quota([{ ...key, state: 'exhausted', reset_at: undefined }]));
    renderApp({ session: adminSession, path: '/admin/quota' });

    expect(await screen.findByTestId('admin-quota-key-row')).toHaveTextContent('Exhausted');
  });

  it('caps the visual progress at the hourly limit while preserving the actual count', async () => {
    server.use(quota([{ ...key, used: 4000, reset_at: undefined }]));
    renderApp({ session: adminSession, path: '/admin/quota' });

    const table = await screen.findByTestId('admin-quota-key-table');
    expect(within(table).getByRole('progressbar', { name: 'Used this hour' })).toHaveAttribute(
      'aria-valuenow',
      '3600',
    );
    expect(within(table).getByText('4,000 / 3,600')).toBeInTheDocument();
  });

  it('shows the empty state and links to the existing keys page', async () => {
    server.use(quota([]));
    renderApp({ session: adminSession, path: '/admin/quota' });

    expect(await screen.findByTestId('admin-quota-empty')).toHaveTextContent('No API keys yet');
    expect(screen.getByRole('link', { name: 'Open my API keys' })).toHaveAttribute('href', '/keys');
  });

  it('retries an initial load failure', async () => {
    let calls = 0;
    server.use(
      http.get('*/admin/quota', () => {
        calls += 1;
        return calls === 1
          ? new HttpResponse(null, { status: 500 })
          : HttpResponse.json({ keys: [key] });
      }),
    );
    renderApp({ session: adminSession, path: '/admin/quota' });

    const retry = await screen.findByTestId('admin-quota-retry');
    fireEvent.click(retry);
    expect(await screen.findByTestId('admin-quota-key-table')).toBeInTheDocument();
  });

  it('keeps the last successful snapshot and marks it stale after refresh fails', async () => {
    let calls = 0;
    server.use(
      http.get('*/admin/quota', () => {
        calls += 1;
        return calls === 1
          ? HttpResponse.json({ keys: [key] })
          : new HttpResponse(null, { status: 500 });
      }),
    );
    renderApp({ session: adminSession, path: '/admin/quota' });

    const table = await screen.findByTestId('admin-quota-key-table');
    fireEvent.click(screen.getByTestId('admin-quota-refresh'));

    expect(await screen.findByTestId('admin-quota-stale')).toBeInTheDocument();
    expect(table).toBeInTheDocument();
    expect(within(table).getByText('AB12CD••••••••')).toBeInTheDocument();
    fireEvent.click(screen.getByTestId('admin-quota-stale-retry'));
    await waitFor(() => expect(screen.getByTestId('admin-quota-refresh')).toBeEnabled());
    expect(within(table).getByText('AB12CD••••••••')).toBeInTheDocument();
  });

  it('clears quota data and shows the forbidden state on 403', async () => {
    server.use(quota([key], 403));
    const { router } = renderApp({ session: adminSession, path: '/admin/quota' });

    expect(await screen.findByTestId('admin-quota-forbidden')).toBeInTheDocument();
    expect(screen.queryByTestId('admin-quota-key-table')).not.toBeInTheDocument();
    fireEvent.click(screen.getByTestId('admin-quota-forbidden-home'));
    await waitFor(() => expect(router.state.location.pathname).toBe('/admin'));
  });

  it('translates the page labels into Vietnamese', async () => {
    server.use(quota([key]));
    renderApp({ session: adminSession, path: '/admin/quota' });

    await screen.findByTestId('admin-quota-key-table');
    fireEvent.click(screen.getByTestId('admin-lang-toggle'));

    await waitFor(() => {
      expect(screen.getByRole('heading', { level: 1, name: 'Hạn mức API' })).toBeInTheDocument();
    });
  });
});
