import { fireEvent, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw/http';
import { describe, expect, it } from 'vitest';
import { adminSession, renderApp, userSession } from '../../../test/render';
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

const queueBody = {
  backlog_by_plan: [
    { plan: 'pro', lane: 'person', backlog_calls: 1200 },
    { plan: 'free', lane: 'sizes', backlog_calls: 34 },
  ],
  oldest_item_age_seconds: 95,
  ready_queue_sizes: [
    { queue: 'sync', ready_jobs: 4 },
    { queue: 'crawl', ready_jobs: 6 },
  ],
};

describe('admin overview page', () => {
  it('shows quota and queue numbers from the existing endpoints', async () => {
    server.use(
      http.get('*/admin/quota', () =>
        HttpResponse.json({
          keys: [quotaKey(1, 'ok'), quotaKey(2, 'exhausted'), quotaKey(3, 'exhausted')],
        }),
      ),
      http.get('*/admin/queue', () => HttpResponse.json(queueBody)),
    );
    renderApp({ session: adminSession, path: '/admin' });

    await waitFor(() =>
      expect(screen.getByTestId('admin-overview-keys-at-quota')).toHaveTextContent('2 of 3 keys'),
    );
    expect(screen.getByTestId('admin-overview-oldest-age')).toHaveTextContent('95 s');
    expect(screen.getByTestId('admin-overview-backlog')).toHaveTextContent('1,234');
    expect(screen.getByTestId('admin-overview-ready')).toHaveTextContent('10');
    expect(screen.getByTestId('admin-overview-link-quota')).toHaveAttribute('href', '/admin/quota');
    expect(screen.getByTestId('admin-overview-pending-sections')).toBeInTheDocument();
  });

  it('shows dashes and an error when neither endpoint answers, then refreshes', async () => {
    let requests = 0;
    server.use(
      http.get('*/admin/quota', () => {
        requests += 1;
        return HttpResponse.json({ code: 'unavailable' }, { status: 503 });
      }),
      http.get('*/admin/queue', () => HttpResponse.json({ code: 'unavailable' }, { status: 503 })),
    );
    renderApp({ session: adminSession, path: '/admin' });

    expect(await screen.findByTestId('admin-overview-error')).toBeInTheDocument();
    expect(screen.getByTestId('admin-overview-backlog')).toHaveTextContent('—');
    const before = requests;
    fireEvent.click(screen.getByTestId('admin-overview-refresh'));
    await waitFor(() => expect(requests).toBeGreaterThan(before));
  });

  it('renders the Vietnamese labels', async () => {
    server.use(
      http.get('*/admin/quota', () => HttpResponse.json({ keys: [] })),
      http.get('*/admin/queue', () => HttpResponse.json(queueBody)),
    );
    renderApp({ session: adminSession, path: '/admin', locale: 'vi' });

    expect(await screen.findByRole('heading', { level: 1, name: 'Tổng quan' })).toBeInTheDocument();
    await waitFor(() =>
      expect(screen.getByTestId('admin-overview-oldest-age')).toHaveTextContent('95 giây'),
    );
  });

  it('shows the forbidden state when the API answers 403', async () => {
    server.use(
      http.get('*/admin/quota', () => HttpResponse.json({ code: 'forbidden' }, { status: 403 })),
      http.get('*/admin/queue', () => HttpResponse.json({ code: 'forbidden' }, { status: 403 })),
    );
    renderApp({ session: adminSession, path: '/admin' });

    expect(await screen.findByTestId('admin-overview-forbidden')).toBeInTheDocument();
    expect(userSession).toBeDefined();
  });
});
