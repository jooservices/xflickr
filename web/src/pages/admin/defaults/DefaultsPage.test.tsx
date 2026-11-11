import { fireEvent, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw/http';
import { describe, expect, it } from 'vitest';
import { adminSession, renderApp, userSession } from '../../../test/render';
import { server } from '../../../test/server';
import { SESSION_EXPIRED_EVENT } from '../../../shared/api/sessionEvents';

const plan = {
  id: 9,
  name: 'Studio',
  weight: 3,
  max_depth_cap: 4,
  max_contacts: 1200,
  ai_quota_per_day: 2500,
  zip_ttl_hours: 48,
  resource_share: 0.375,
};
const plans = (data: object = [plan], status = 200) =>
  http.get('*/admin/plans', () => HttpResponse.json(data, { status }));
const open = (locale: 'en' | 'vi' = 'en') =>
  renderApp({ session: adminSession, path: '/admin/defaults', locale });

describe('Defaults plans', () => {
  it('renders server plans and localized values', async () => {
    server.use(plans());
    open();
    const row = await screen.findByTestId('admin-defaults-plan-Studio');
    for (const value of ['Studio', '3', '4', '1,200', '2,500', '48', '37.5%'])
      expect(row).toHaveTextContent(value);
    expect(screen.getByRole('table', { name: 'Plans' })).toBeInTheDocument();
    expect(screen.getAllByRole('columnheader')).toHaveLength(7);
  });
  it('shows loading and empty', async () => {
    let release!: () => void;
    const gate = new Promise<void>((resolve) => {
      release = resolve;
    });
    server.use(
      http.get('*/admin/plans', async () => {
        await gate;
        return HttpResponse.json([]);
      }),
    );
    open();
    expect(await screen.findByTestId('admin-defaults-loading')).toHaveAttribute(
      'aria-busy',
      'true',
    );
    release();
    expect(await screen.findByTestId('admin-defaults-empty')).toHaveTextContent(
      'No plans available',
    );
  });
  it('retries initial failure', async () => {
    server.use(plans({ message: 'private error' }, 500));
    open();
    expect(await screen.findByTestId('admin-defaults-error')).toHaveTextContent(
      'Could not load plans',
    );
    expect(screen.queryByText('private error')).not.toBeInTheDocument();
    server.use(plans());
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    expect(await screen.findByTestId('admin-defaults-plan-Studio')).toBeInTheDocument();
  });
  it('retains stale data on refresh failure and refreshes again', async () => {
    server.use(plans());
    open();
    await screen.findByTestId('admin-defaults-plan-Studio');
    server.use(plans({}, 503));
    fireEvent.click(screen.getByTestId('admin-defaults-refresh'));
    expect(await screen.findByTestId('admin-defaults-stale')).toHaveAttribute('role', 'status');
    expect(screen.getByTestId('admin-defaults-plan-Studio')).toBeInTheDocument();
    server.use(plans([{ ...plan, name: 'Updated' }]));
    fireEvent.click(screen.getByTestId('admin-defaults-refresh'));
    expect(await screen.findByTestId('admin-defaults-plan-Updated')).toBeInTheDocument();
    expect(screen.queryByTestId('admin-defaults-stale')).not.toBeInTheDocument();
  });
  it('clears cached data on 403', async () => {
    server.use(plans());
    const { client, router } = open();
    await screen.findByTestId('admin-defaults-plan-Studio');
    server.use(plans({}, 403));
    fireEvent.click(screen.getByTestId('admin-defaults-refresh'));
    expect(await screen.findByTestId('admin-defaults-forbidden')).toBeInTheDocument();
    await waitFor(() => expect(client.getQueryData(['admin', 'plans'])).toBeUndefined());
    expect(screen.queryByText('Studio')).not.toBeInTheDocument();
    fireEvent.click(screen.getByTestId('admin-defaults-forbidden-home'));
    await waitFor(() => expect(router.state.location.pathname).toBe('/admin'));
  });
  it('handles 401 using session flow', async () => {
    let expired = false;
    const listener = () => {
      expired = true;
    };
    window.addEventListener(SESSION_EXPIRED_EVENT, listener);
    try {
      server.use(plans({}, 401));
      open();
      await waitFor(() => expect(expired).toBe(true));
    } finally {
      window.removeEventListener(SESSION_EXPIRED_EVENT, listener);
    }
  });
  it('renders Vietnamese', async () => {
    server.use(plans());
    open('vi');
    const row = await screen.findByTestId('admin-defaults-plan-Studio');
    expect(row).toHaveTextContent('1.200');
    expect(row).toHaveTextContent('37,5%');
    expect(screen.getByRole('heading', { name: 'Mặc định và gói' })).toBeInTheDocument();
    expect(screen.getByRole('columnheader', { name: 'Tỷ lệ tài nguyên' })).toBeInTheDocument();
  });
  it('does not expose edit actions or placeholder links', async () => {
    server.use(plans());
    open();
    const page = await screen.findByTestId('admin-defaults-page');
    await screen.findByTestId('admin-defaults-plan-Studio');
    expect(page.querySelectorAll('input, select, textarea, a')).toHaveLength(0);
    expect(page.querySelectorAll('button')).toHaveLength(1);
    expect(page).toHaveTextContent('Global defaults and feature flags are not available here yet.');
    expect(page).not.toHaveTextContent('Live');
  });
  it('does not request plans for a USER', async () => {
    let calls = 0;
    server.use(
      http.get('*/admin/plans', () => {
        calls++;
        return HttpResponse.json([plan]);
      }),
    );
    renderApp({ session: userSession, path: '/admin/defaults' });
    expect(await screen.findByTestId('shell-forbidden')).toBeInTheDocument();
    expect(calls).toBe(0);
  });
});
