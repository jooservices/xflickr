import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { onlineManager } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw/http';
import { describe, expect, it, vi } from 'vitest';
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
    expect(
      within(table).getByRole('columnheader', { name: 'Next call expires at' }),
    ).toBeInTheDocument();
    expect(within(table).getByTestId('admin-quota-cooldown').textContent).toMatch(/^in /);
    expect(
      screen.getByText(
        'Lane totals, burst values and key controls are not included in the available API response.',
      ),
    ).toBeInTheDocument();
    expect(screen.getByTestId('admin-quota-node-table')).toHaveTextContent(
      'Per-node quota data is not included',
    );
  });

  it('renders key and user identifiers without locale grouping', async () => {
    server.use(quota([{ ...key, key_id: 1000, user_id: 2000 }]));
    renderApp({ session: adminSession, path: '/admin/quota' });

    const row = await screen.findByTestId('admin-quota-key-row');
    expect(within(row).getByText('#1000')).toBeInTheDocument();
    expect(within(row).getByText('User #2000')).toBeInTheDocument();
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

  it('pauses shared quota polling and fetches a fresh snapshot when Live resumes', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      let used = 100;
      let calls = 0;
      server.use(
        http.get('*/admin/quota', () => {
          calls += 1;
          return HttpResponse.json({ keys: [{ ...key, used }] });
        }),
      );
      renderApp({ session: adminSession, path: '/admin/quota' });

      const table = await screen.findByTestId('admin-quota-key-table');
      expect(calls).toBe(1);
      expect(within(table).getByText('100 / 3,600')).toBeInTheDocument();

      const liveToggle = screen.getByTestId('admin-live-toggle');
      fireEvent.click(liveToggle);
      expect(liveToggle).toHaveAttribute('aria-pressed', 'false');

      used = 250;
      await vi.advanceTimersByTimeAsync(10_000);

      expect(calls).toBe(1);
      expect(within(table).getByText('100 / 3,600')).toBeInTheDocument();

      fireEvent.click(liveToggle);
      expect(liveToggle).toHaveAttribute('aria-pressed', 'true');
      expect(await within(table).findByText('250 / 3,600')).toBeInTheDocument();
      expect(calls).toBe(2);
    } finally {
      vi.useRealTimers();
    }
  });

  it('cancels an in-flight quota refresh when Live pauses', async () => {
    let calls = 0;
    let requestSignal: AbortSignal | undefined;
    let releaseLateResponse: (() => void) | undefined;
    let lateResponseReturned = false;
    const lateResponseGate = new Promise<void>((resolve) => {
      releaseLateResponse = resolve;
    });
    server.use(
      http.get('*/admin/quota', async ({ request }) => {
        calls += 1;
        if (calls === 1) return HttpResponse.json({ keys: [{ ...key, used: 100 }] });
        if (calls === 2) {
          requestSignal = request.signal;
          await lateResponseGate;
          lateResponseReturned = true;
          return HttpResponse.json({ keys: [{ ...key, used: 250 }] });
        }
        return HttpResponse.json({ keys: [{ ...key, used: 400 }] });
      }),
    );
    renderApp({ session: adminSession, path: '/admin/quota' });

    const table = await screen.findByTestId('admin-quota-key-table');
    fireEvent.click(screen.getByTestId('admin-quota-refresh'));
    await waitFor(() => expect(calls).toBe(2));
    fireEvent.click(screen.getByTestId('admin-live-toggle'));

    await waitFor(() => expect(requestSignal?.aborted).toBe(true));
    expect(within(table).getByText('100 / 3,600')).toBeInTheDocument();
    releaseLateResponse?.();
    await waitFor(() => expect(lateResponseReturned).toBe(true));
    expect(within(table).getByText('100 / 3,600')).toBeInTheDocument();
    expect(within(table).queryByText('250 / 3,600')).not.toBeInTheDocument();

    fireEvent.click(screen.getByTestId('admin-live-toggle'));
    expect(await within(table).findByText('400 / 3,600')).toBeInTheDocument();
    expect(calls).toBe(3);
  });

  it.each([
    {
      locale: 'en' as const,
      title: 'No API quota snapshot yet.',
      body: 'Live is off or the browser is offline. Select Load once to fetch one snapshot, or reconnect and resume Live.',
      button: 'Load once',
      usage: '200 / 3,600',
    },
    {
      locale: 'vi' as const,
      title: 'Chưa có bản chụp hạn mức API.',
      body: 'Live đang tắt hoặc trình duyệt ngoại tuyến. Chọn Tải một lần để lấy dữ liệu, hoặc kết nối mạng và bật lại Live.',
      button: 'Tải một lần',
      usage: '200 / 3.600',
    },
  ])(
    'shows a localized one-shot action when Live pauses before the first snapshot ($locale)',
    async ({ locale, title, body, button, usage }) => {
      let calls = 0;
      let releaseInitialRequest: (() => void) | undefined;
      let requestSignal: AbortSignal | undefined;
      let initialResponseReturned = false;
      const initialRequestGate = new Promise<void>((resolve) => {
        releaseInitialRequest = resolve;
      });
      server.use(
        http.get('*/admin/quota', async ({ request }) => {
          calls += 1;
          if (calls === 1) {
            requestSignal = request.signal;
            await initialRequestGate;
            initialResponseReturned = true;
            return HttpResponse.json({ keys: [{ ...key, used: 100 }] });
          }
          return HttpResponse.json({ keys: [{ ...key, used: 200 }] });
        }),
      );
      renderApp({ session: adminSession, path: '/admin/quota', locale });

      await waitFor(() => expect(calls).toBe(1));
      fireEvent.click(screen.getByTestId('admin-live-toggle'));

      const waiting = await screen.findByTestId('admin-quota-waiting');
      await waitFor(() => expect(requestSignal?.aborted).toBe(true));
      expect(waiting).toHaveTextContent(title);
      expect(waiting).toHaveTextContent(body);
      expect(waiting).toHaveTextContent('API');
      expect(waiting).toHaveTextContent('Live');

      releaseInitialRequest?.();
      await waitFor(() => expect(initialResponseReturned).toBe(true));
      const loadButton = screen.getByTestId('admin-quota-waiting-load-once');
      expect(loadButton).toHaveTextContent(button);
      fireEvent.click(loadButton);
      expect(await screen.findByTestId('admin-quota-key-table')).toBeInTheDocument();
      expect(screen.getByTestId('admin-quota-key-table')).toHaveTextContent(usage);
      expect(calls).toBe(2);
    },
  );

  it.each([
    {
      locale: 'en' as const,
      title: 'No API quota snapshot yet.',
      button: 'Load once',
      error: 'Could not load API quota.',
    },
    {
      locale: 'vi' as const,
      title: 'Chưa có bản chụp hạn mức API.',
      button: 'Tải một lần',
      error: 'Không thể tải hạn mức API.',
    },
  ])(
    'attempts and retries a one-shot quota load immediately while offline ($locale)',
    async ({ locale, title, button, error }) => {
      let calls = 0;
      let unmount: () => void = () => undefined;
      onlineManager.setOnline(false);
      server.use(
        http.get('*/admin/quota', () => {
          calls += 1;
          return calls === 1
            ? new HttpResponse(null, { status: 503 })
            : HttpResponse.json({ keys: [key] });
        }),
      );

      try {
        const app = renderApp({ session: adminSession, path: '/admin/quota', locale });
        unmount = app.unmount;
        const waiting = await screen.findByTestId('admin-quota-waiting');
        expect(waiting).toHaveTextContent(title);
        expect(waiting).toHaveTextContent('Live');
        expect(calls).toBe(0);
        const liveToggle = screen.getByTestId('admin-live-toggle');
        fireEvent.click(liveToggle);
        expect(liveToggle).toHaveAttribute('aria-pressed', 'false');

        const loadButton = screen.getByTestId('admin-quota-waiting-load-once');
        expect(loadButton).toHaveTextContent(button);
        fireEvent.click(loadButton);
        expect(await screen.findByTestId('admin-quota-error')).toHaveTextContent(error);
        expect(calls).toBe(1);
        fireEvent.click(screen.getByTestId('admin-quota-retry'));
        const table = await screen.findByTestId('admin-quota-key-table');
        expect(table).toHaveTextContent(key.label);
        expect(calls).toBe(2);
        expect(liveToggle).toHaveAttribute('aria-pressed', 'false');
      } finally {
        unmount();
        onlineManager.setOnline(true);
      }
    },
  );

  it('returns polling to ten seconds after a successful refresh', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      let calls = 0;
      server.use(
        http.get('*/admin/quota', () => {
          calls += 1;
          return calls === 2
            ? new HttpResponse(null, { status: 500 })
            : HttpResponse.json({ keys: [{ ...key, used: calls * 100 }] });
        }),
      );
      renderApp({ session: adminSession, path: '/admin/quota' });

      await screen.findByTestId('admin-quota-key-table');
      await vi.advanceTimersByTimeAsync(10_000);
      await waitFor(() => expect(calls).toBe(2));
      await vi.advanceTimersByTimeAsync(20_000);
      await waitFor(() => expect(calls).toBe(3));
      await vi.advanceTimersByTimeAsync(10_000);
      await waitFor(() => expect(calls).toBe(4));
    } finally {
      vi.useRealTimers();
    }
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

    const empty = await screen.findByTestId('admin-quota-empty');
    expect(empty).toHaveTextContent('No valid API keys available');
    expect(empty).toHaveTextContent('when a valid Flickr API key is available');
    expect(screen.getByRole('link', { name: 'Open my API keys' })).toHaveAttribute('href', '/keys');

    fireEvent.click(screen.getByTestId('admin-lang-toggle'));
    await waitFor(() => {
      expect(empty).toHaveTextContent('Chưa có khóa API hợp lệ');
      expect(empty).toHaveTextContent('khi có khóa API Flickr hợp lệ');
    });
    expect(screen.getByRole('link', { name: 'Mở khóa API của tôi' })).toHaveAttribute(
      'href',
      '/keys',
    );
  });

  it('shows stale state and retry for an empty snapshot after refresh fails', async () => {
    let calls = 0;
    server.use(
      http.get('*/admin/quota', () => {
        calls += 1;
        if (calls === 1 || calls > 2) return HttpResponse.json({ keys: [] });
        return new HttpResponse(null, { status: 500 });
      }),
    );
    renderApp({ session: adminSession, path: '/admin/quota' });

    const empty = await screen.findByTestId('admin-quota-empty');
    fireEvent.click(screen.getByTestId('admin-quota-refresh'));
    expect(await screen.findByTestId('admin-quota-stale')).toBeInTheDocument();
    expect(screen.getByTestId('admin-quota-stale-retry')).toBeInTheDocument();
    expect(empty).toBeInTheDocument();

    fireEvent.click(screen.getByTestId('admin-quota-stale-retry'));
    await waitFor(() => expect(screen.queryByTestId('admin-quota-stale')).not.toBeInTheDocument());
    expect(empty).toBeInTheDocument();
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

    const table = await screen.findByTestId('admin-quota-key-table');
    await waitFor(() => {
      expect(screen.getByRole('heading', { level: 1, name: 'Hạn mức API' })).toBeInTheDocument();
      expect(
        within(table).getByRole('columnheader', {
          name: 'Thời điểm lượt gọi tiếp theo hết hạn',
        }),
      ).toBeInTheDocument();
    });
  });
});
