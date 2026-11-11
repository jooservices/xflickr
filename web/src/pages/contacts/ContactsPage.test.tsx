import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { I18nextProvider } from 'react-i18next';
import { MemoryRouter } from 'react-router';
import { http, HttpResponse } from 'msw/http';
import { describe, expect, it, vi } from 'vitest';
import { SessionContext } from '../../entities';
import { userSession } from '../../test/render';
import { server } from '../../test/server';
import { I18nBridge, i18nInstance } from '../../shared';
import type { Locale } from '../../shared';
import { SESSION_EXPIRED_EVENT } from '../../shared/api/sessionEvents';
import { ContactsPage } from './ContactsPage';

const json = (body: unknown, status = 200) => HttpResponse.json(body as never, { status });

const settings = { depth: 3, depth_default: 2, depth_max: 4 };

function open(path = '/contacts', locale: Locale = 'en') {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const result = render(
    <QueryClientProvider client={queryClient}>
      <I18nextProvider i18n={i18nInstance}>
        <I18nBridge initialLocale={locale}>
          <SessionContext.Provider value={{ session: userSession, retry: () => undefined }}>
            <MemoryRouter initialEntries={[path]}>
              <ContactsPage />
            </MemoryRouter>
          </SessionContext.Provider>
        </I18nBridge>
      </I18nextProvider>
    </QueryClientProvider>,
  );

  return { ...result, queryClient };
}

describe('ContactsPage', () => {
  it('shows effective depth and focuses the heading', async () => {
    server.use(http.get('*/me/crawl/settings', () => json(settings)));
    open();

    const heading = await screen.findByTestId('user-contacts-title');
    expect(heading).toHaveTextContent('Contacts');
    expect(document.title).toBe('Contacts · XFlickr');
    expect(await screen.findByTestId('user-contacts-description')).toHaveTextContent(
      'Crawl depth: 3.',
    );
    await waitFor(() => expect(heading).toHaveFocus());
    expect(screen.getByTestId('user-contacts-unavailable')).toBeInTheDocument();
    expect(
      within(screen.getByTestId('user-contacts-page')).queryByRole('link'),
    ).not.toBeInTheDocument();
    expect(
      within(screen.getByTestId('user-contacts-page')).queryByRole('button'),
    ).not.toBeInTheDocument();
  });

  it('shows loading without a fabricated depth', async () => {
    let release: () => void = () => undefined;
    const gate = new Promise<void>((resolve) => {
      release = resolve;
    });
    server.use(
      http.get('*/me/crawl/settings', async () => {
        await gate;
        return json(settings);
      }),
    );
    open();

    expect(await screen.findByTestId('user-contacts-loading')).toHaveAttribute('role', 'status');
    expect(screen.getByTestId('user-contacts-loading')).toHaveAccessibleName('Loading contacts');
    expect(screen.queryByTestId('user-contacts-description')).not.toBeInTheDocument();
    expect(screen.queryByText(/depth\s*\d/i)).not.toBeInTheDocument();

    release();
    expect(await screen.findByTestId('user-contacts-unavailable')).toBeInTheDocument();
  });

  it('retries failed settings and shows the returned depth', async () => {
    let requests = 0;
    server.use(
      http.get('*/me/crawl/settings', () => {
        requests += 1;
        return requests === 1
          ? json({ detail: 'raw server failure' }, 500)
          : json({ ...settings, depth: 5 });
      }),
    );
    open();

    expect(await screen.findByTestId('user-contacts-error')).toHaveTextContent(
      'Could not load contacts',
    );
    expect(screen.getByTestId('user-contacts-error')).toHaveAttribute('role', 'alert');
    expect(screen.getByTestId('user-contacts-error')).not.toHaveTextContent('raw server failure');
    fireEvent.click(screen.getByTestId('user-contacts-retry'));

    expect(await screen.findByTestId('user-contacts-description')).toHaveTextContent(
      'Crawl depth: 5.',
    );
    expect(requests).toBe(2);
  });

  it('hides cached depth when a settings refetch is forbidden', async () => {
    server.use(http.get('*/me/crawl/settings', () => json(settings)));
    const { queryClient } = open();

    expect(await screen.findByTestId('user-contacts-description')).toHaveTextContent(
      'Crawl depth: 3.',
    );
    server.use(http.get('*/me/crawl/settings', () => json({ code: 'forbidden' }, 403)));
    await act(() => queryClient.invalidateQueries({ queryKey: ['crawl', 'settings'] }));

    expect(await screen.findByTestId('user-contacts-forbidden')).toHaveTextContent(
      'You cannot view these contacts',
    );
    expect(screen.queryByTestId('user-contacts-description')).not.toBeInTheDocument();
    expect(screen.queryByTestId('user-contacts-unavailable')).not.toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Back to home' })).toHaveAttribute('href', '/');
  });

  it('shows translated Vietnamese strings', async () => {
    server.use(http.get('*/me/crawl/settings', () => json(settings)));
    const { queryClient } = open('/contacts', 'vi');

    expect(await screen.findByRole('heading', { level: 1, name: 'Liên hệ' })).toBeInTheDocument();
    expect(await screen.findByTestId('user-contacts-description')).toHaveTextContent(
      'Độ sâu thu thập: 3.',
    );
    expect(screen.getByTestId('user-contacts-unavailable')).toHaveTextContent(
      'Danh sách liên hệ hiện chưa khả dụng.',
    );
    expect(document.title).toBe('Liên hệ · XFlickr');

    server.use(http.get('*/me/crawl/settings', () => json({ code: 'server_error' }, 500)));
    await act(() => queryClient.invalidateQueries({ queryKey: ['crawl', 'settings'] }));
    expect(await screen.findByTestId('user-contacts-error')).toHaveTextContent(
      'Không thể tải liên hệ',
    );
    expect(screen.getByTestId('user-contacts-retry')).toHaveTextContent('Thử lại');

    server.use(http.get('*/me/crawl/settings', () => json({ code: 'forbidden' }, 403)));
    fireEvent.click(screen.getByTestId('user-contacts-retry'));
    expect(await screen.findByTestId('user-contacts-forbidden')).toHaveTextContent(
      'Bạn không thể xem các liên hệ này',
    );
    expect(screen.getByRole('link', { name: 'Về trang chủ' })).toBeInTheDocument();
  });

  it('signals the shared expired-session flow on HTTP 401', async () => {
    const expired = vi.fn();
    window.addEventListener(SESSION_EXPIRED_EVENT, expired);
    server.use(http.get('*/me/crawl/settings', () => json({ code: 'unauthenticated' }, 401)));
    open();

    await waitFor(() => expect(expired).toHaveBeenCalledOnce());
    window.removeEventListener(SESSION_EXPIRED_EVENT, expired);
  });

  it('treats a malformed settings response as a retryable error', async () => {
    server.use(
      http.get(
        '*/me/crawl/settings',
        () =>
          new HttpResponse('<html></html>', {
            status: 200,
            headers: { 'Content-Type': 'text/html' },
          }),
      ),
    );
    open();

    expect(await screen.findByTestId('user-contacts-error')).toHaveTextContent(
      'Could not load contacts',
    );
    expect(screen.getByTestId('user-contacts-retry')).toBeEnabled();
  });
});
