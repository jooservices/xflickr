import { fireEvent, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw/http';
import { describe, expect, it } from 'vitest';
import { adminSession, renderApp } from '../../../test/render';
import { server } from '../../../test/server';

const quotaKey = (id: number, state: 'ok' | 'cooling' | 'exhausted') => ({
  key_id: id,
  label: `Key ${String(id)}`,
  node: 'node-1',
  used: 10,
  limit: 3600,
  state,
  trend: 'steady',
});

describe('admin overview page', () => {
  it('shows the keys at quota from the existing quota endpoint and no placeholder link', async () => {
    server.use(
      http.get('*/admin/quota', () =>
        HttpResponse.json({
          keys: [quotaKey(1, 'ok'), quotaKey(2, 'exhausted'), quotaKey(3, 'exhausted')],
        }),
      ),
    );
    renderApp({ session: adminSession, path: '/admin' });

    await waitFor(() =>
      expect(screen.getByTestId('admin-overview-keys-at-quota')).toHaveTextContent('2 of 3 keys'),
    );
    expect(screen.getByTestId('admin-overview-link-quota')).toHaveAttribute('href', '/admin/quota');
    expect(screen.queryByText('Pipeline')).not.toBeInTheDocument();
    expect(screen.getByTestId('admin-overview-pending-sections')).toBeInTheDocument();
  });

  it('counts a cooling key that already used its whole limit as at quota', async () => {
    server.use(
      http.get('*/admin/quota', () =>
        HttpResponse.json({
          keys: [
            quotaKey(1, 'ok'),
            { ...quotaKey(2, 'cooling'), used: 3600 },
            { ...quotaKey(3, 'cooling'), used: 100 },
          ],
        }),
      ),
    );
    renderApp({ session: adminSession, path: '/admin' });

    await waitFor(() =>
      expect(screen.getByTestId('admin-overview-keys-at-quota')).toHaveTextContent('1 of 3 keys'),
    );
  });

  it('shows a dash and an error when the quota endpoint fails, then refreshes', async () => {
    let requests = 0;
    server.use(
      http.get('*/admin/quota', () => {
        requests += 1;
        return HttpResponse.json({ code: 'unavailable' }, { status: 503 });
      }),
    );
    renderApp({ session: adminSession, path: '/admin' });

    expect(await screen.findByTestId('admin-overview-error')).toBeInTheDocument();
    expect(screen.getByTestId('admin-overview-keys-at-quota')).toHaveTextContent('—');
    const before = requests;
    fireEvent.click(screen.getByTestId('admin-overview-refresh'));
    await waitFor(() => expect(requests).toBeGreaterThan(before));
  });

  it('keeps the last numbers and says so when a refresh fails', async () => {
    let requests = 0;
    server.use(
      http.get('*/admin/quota', () => {
        requests += 1;
        return requests === 1
          ? HttpResponse.json({ keys: [quotaKey(1, 'exhausted')] })
          : HttpResponse.json({ code: 'unavailable' }, { status: 503 });
      }),
    );
    renderApp({ session: adminSession, path: '/admin' });

    await waitFor(() =>
      expect(screen.getByTestId('admin-overview-keys-at-quota')).toHaveTextContent('1 of 1 keys'),
    );
    fireEvent.click(screen.getByTestId('admin-overview-refresh'));

    expect(await screen.findByTestId('admin-overview-stale')).toBeInTheDocument();
    expect(screen.getByTestId('admin-overview-keys-at-quota')).toHaveTextContent('1 of 1 keys');
  });

  it('renders the Vietnamese labels', async () => {
    server.use(http.get('*/admin/quota', () => HttpResponse.json({ keys: [] })));
    renderApp({ session: adminSession, path: '/admin', locale: 'vi' });

    expect(await screen.findByRole('heading', { level: 1, name: 'Tổng quan' })).toBeInTheDocument();
    expect(screen.getByText('Hạn mức khóa trong một cái nhìn.')).toBeInTheDocument();
  });

  it('shows the forbidden state when the API answers 403', async () => {
    server.use(
      http.get('*/admin/quota', () => HttpResponse.json({ code: 'forbidden' }, { status: 403 })),
    );
    renderApp({ session: adminSession, path: '/admin' });

    expect(await screen.findByTestId('admin-overview-forbidden')).toBeInTheDocument();
  });
});
