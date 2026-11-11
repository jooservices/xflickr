import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw/http';
import { describe, expect, it } from 'vitest';
import { adminSession, renderApp } from '../../../test/render';
import { server } from '../../../test/server';

const openedAt = '2026-11-12T01:00:00Z';
const incident = {
  id: 123,
  severity: 'critical',
  first_seen: '2026-11-12T00:00:00Z',
  last_seen: openedAt,
  count: 4,
};

const incidents = (items: unknown[], status = 200) =>
  http.get('*/admin/incidents', () =>
    HttpResponse.json({ items, next_cursor: '', server_time: openedAt }, { status }),
  );

describe('admin issues page', () => {
  it('renders safe open incident metadata and hides unapproved detector content', async () => {
    server.use(
      http.get('*/admin/incidents', () =>
        HttpResponse.json({
          items: [
            {
              ...incident,
              title: 'Unsafe detector title',
              scope: 'key:secret-scope',
              detail: { human_step: 'Unsafe detector instructions' },
            },
          ],
          next_cursor: '',
          server_time: openedAt,
        }),
      ),
    );
    renderApp({ session: adminSession, path: '/admin/issues' });

    const table = await screen.findByTestId('admin-issues-table');
    expect(screen.getByRole('heading', { level: 1, name: 'Issues' })).toBeInTheDocument();
    expect(within(table).getByText('Incident #123')).toBeInTheDocument();
    expect(within(table).getByText('Critical')).toBeInTheDocument();
    expect(within(table).getByText('4')).toBeInTheDocument();
    expect(table).not.toHaveTextContent('Unsafe detector');
    expect(table).not.toHaveTextContent('key:secret-scope');
    expect(screen.getByTestId('admin-nav-issues')).toBeInTheDocument();
    expect(screen.queryByTestId('admin-nav-events')).not.toBeInTheDocument();
  });

  it('renders a real empty state when no open incidents exist', async () => {
    server.use(incidents([]));
    renderApp({ session: adminSession, path: '/admin/issues' });

    expect(await screen.findByTestId('admin-issues-empty')).toHaveTextContent('No open issues');
    expect(screen.queryByTestId('admin-issues-table')).not.toBeInTheDocument();
  });

  it('announces the loading state while the incident read is pending', async () => {
    let release: (() => void) | undefined;
    const pending = new Promise<void>((resolve) => {
      release = resolve;
    });
    server.use(
      http.get('*/admin/incidents', async () => {
        await pending;
        return HttpResponse.json({ items: [], next_cursor: '', server_time: openedAt });
      }),
    );
    renderApp({ session: adminSession, path: '/admin/issues' });

    expect(await screen.findByTestId('admin-issues-loading')).toHaveTextContent('Loading issues');
    release?.();
    expect(await screen.findByTestId('admin-issues-empty')).toBeInTheDocument();
  });

  it('shows an error instead of an empty list and retries the failed read', async () => {
    let calls = 0;
    server.use(
      http.get('*/admin/incidents', () => {
        calls += 1;
        return calls === 1
          ? HttpResponse.json({ message: 'failed' }, { status: 500 })
          : HttpResponse.json({ items: [incident], next_cursor: '', server_time: openedAt });
      }),
    );
    renderApp({ session: adminSession, path: '/admin/issues' });

    const error = await screen.findByTestId('admin-issues-error');
    expect(error).toHaveTextContent('Issues could not be loaded');
    fireEvent.click(within(error).getByTestId('admin-issues-error-retry'));
    expect(await screen.findByTestId('admin-issues-table')).toHaveTextContent('Incident #123');
    expect(calls).toBe(2);
  });

  it('shows a forbidden state when the incident endpoint denies access', async () => {
    server.use(incidents([], 403));
    renderApp({ session: adminSession, path: '/admin/issues' });

    expect(await screen.findByTestId('admin-issues-forbidden')).toHaveTextContent(
      'Admin access required',
    );
    expect(screen.queryByTestId('admin-issues-table')).not.toBeInTheDocument();
  });

  it('loads additional incidents from the opaque next cursor', async () => {
    let calls = 0;
    server.use(
      http.get('*/admin/incidents', ({ request }) => {
        calls += 1;
        const cursor = new URL(request.url).searchParams.get('cursor');
        if (cursor === null) {
          return HttpResponse.json({
            items: [incident],
            next_cursor: 'next-page',
            server_time: openedAt,
          });
        }
        return HttpResponse.json({
          items: [{ ...incident, id: 124, severity: 'warning' }],
          next_cursor: '',
          server_time: openedAt,
        });
      }),
    );
    renderApp({ session: adminSession, path: '/admin/issues' });

    const table = await screen.findByTestId('admin-issues-table');
    fireEvent.click(screen.getByTestId('admin-issues-load-more'));
    expect(await within(table).findByText('Incident #124')).toBeInTheDocument();
    expect(calls).toBe(2);
  });

  it('keeps the last successful rows when a refresh fails', async () => {
    let calls = 0;
    server.use(
      http.get('*/admin/incidents', () => {
        calls += 1;
        return calls === 1
          ? HttpResponse.json({ items: [incident], next_cursor: '', server_time: openedAt })
          : HttpResponse.json({ message: 'failed' }, { status: 500 });
      }),
    );
    renderApp({ session: adminSession, path: '/admin/issues' });

    const table = await screen.findByTestId('admin-issues-table');
    fireEvent.click(screen.getByTestId('admin-issues-refresh'));
    await waitFor(() => expect(screen.getByTestId('admin-issues-stale')).toBeInTheDocument());
    expect(within(table).getByText('Incident #123')).toBeInTheDocument();
    expect(screen.queryByTestId('admin-issues-empty')).not.toBeInTheDocument();
  });

  it('localizes the empty state in Vietnamese', async () => {
    server.use(incidents([]));
    renderApp({ session: adminSession, path: '/admin/issues', locale: 'vi' });

    expect(await screen.findByTestId('admin-issues-empty')).toHaveTextContent(
      'Không có sự cố đang mở',
    );
  });
});
