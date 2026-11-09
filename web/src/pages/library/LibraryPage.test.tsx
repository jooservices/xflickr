import { fireEvent, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw/http';
import { describe, expect, it, vi } from 'vitest';
import { server } from '../../test/server';
import { renderApp, userSession } from '../../test/render';

const photo = (id: number, owner = 'linh@N00', patch: Record<string, unknown> = {}) => ({
  id,
  owner_nsid: owner,
  title: `Photo ${String(id)}`,
  description: '',
  labels: [],
  visibility: 'public',
  sizes_status: 'confirmed',
  sizes: [{ label: 'Medium', width: 320, height: 240, url: `https://images.test/${String(id)}` }],
  ai_status: 'ready',
  ...patch,
});

const favorite = (id: number, owner?: string, patch?: Record<string, unknown>) => ({
  photo: photo(id, owner, patch),
  favorite_at: '2026-11-01T00:00:00Z',
});

const json = (body: unknown, status = 200) => HttpResponse.json(body as never, { status });

const open = (locale: 'en' | 'vi' = 'en') =>
  renderApp({ session: userSession, path: '/library', locale });

describe('LibraryPage', () => {
  it('renders catalog favorites and pages with the returned cursor', async () => {
    const cursors: string[] = [];
    server.use(
      http.get('*/favorites', ({ request }) => {
        const cursor = new URL(request.url).searchParams.get('cursor') ?? '';
        cursors.push(cursor);
        return cursor === ''
          ? json({ items: [favorite(1), favorite(2, 'morgan@N00')], next_cursor: 'page-two' })
          : json({ items: [favorite(3)] });
      }),
    );
    open();

    expect(await screen.findByRole('heading', { level: 1, name: 'Library' })).toBeInTheDocument();
    expect(await screen.findByTestId('user-library-photo-1')).toBeInTheDocument();
    expect(screen.getByTestId('user-library-photo-2')).toBeInTheDocument();
    expect(screen.getByTestId('user-library-grid')).toHaveAttribute('aria-busy', 'false');
    fireEvent.click(screen.getByTestId('user-library-load-more'));

    expect(await screen.findByTestId('user-library-photo-3')).toBeInTheDocument();
    expect(cursors).toEqual(['', 'page-two']);
  });

  it('filters the loaded catalog page by contact, sizes and AI status', async () => {
    server.use(
      http.get('*/favorites', () =>
        json({
          items: [
            favorite(1, 'linh@N00'),
            favorite(2, 'morgan@N00', { sizes_status: 'none' }),
            favorite(3, 'morgan@N00', { ai_status: 'none' }),
            favorite(4, 'morgan@N00'),
          ],
        }),
      ),
    );
    open();
    await screen.findByTestId('user-library-photo-1');

    fireEvent.click(screen.getByRole('checkbox', { name: 'morgan@N00' }));
    fireEvent.change(screen.getByTestId('user-library-contact-search'), {
      target: { value: 'morg' },
    });
    await waitFor(() =>
      expect(screen.queryByRole('checkbox', { name: 'linh@N00' })).not.toBeInTheDocument(),
    );
    fireEvent.click(screen.getByTestId('user-library-filter-sizes'));
    fireEvent.click(screen.getByTestId('user-library-filter-captioned'));

    expect(screen.getByTestId('user-library-photo-4')).toBeInTheDocument();
    expect(screen.queryByTestId('user-library-photo-1')).not.toBeInTheDocument();
    expect(screen.queryByTestId('user-library-photo-2')).not.toBeInTheDocument();
    expect(screen.queryByTestId('user-library-photo-3')).not.toBeInTheDocument();

    fireEvent.click(screen.getByTestId('user-library-filter-clear'));
    expect(screen.getByTestId('user-library-photo-1')).toBeInTheDocument();
    expect(screen.getByTestId('user-library-photo-2')).toBeInTheDocument();
    expect(screen.getByTestId('user-library-photo-3')).toBeInTheDocument();
  });

  it('selects visible photos and clears the selection', async () => {
    server.use(
      http.get('*/favorites', () => json({ items: [favorite(21), favorite(22), favorite(23)] })),
    );
    open();
    await screen.findByTestId('user-library-photo-21');
    fireEvent.click(screen.getByTestId('user-library-select-mode'));
    fireEvent.click(screen.getByTestId('user-library-select-visible'));

    expect(screen.getByTestId('user-library-selection-status')).toHaveTextContent('3');
    expect(screen.getByTestId('user-library-photo-select-21')).toBeChecked();
    expect(screen.getByTestId('user-library-photo-select-22')).toBeChecked();
    expect(screen.getByTestId('user-library-photo-select-23')).toBeChecked();
    fireEvent.click(screen.getByTestId('user-library-clear-selection'));
    expect(screen.getByTestId('user-library-selection-status')).toHaveTextContent('0');
  });

  it('limits the selection to 500 and disables adding another photo', async () => {
    const total = 501;
    server.use(
      http.get('*/favorites', ({ request }) => {
        const start = Number(new URL(request.url).searchParams.get('cursor') ?? '0');
        const end = Math.min(start + 100, total);
        const items = Array.from({ length: end - start }, (_, index) => {
          const offset = start + index;
          return favorite(offset + 100, offset === 500 ? 'unselected@N00' : undefined);
        });
        return json({ items, ...(end < total ? { next_cursor: String(end) } : {}) });
      }),
    );
    open();
    await waitFor(() =>
      expect(screen.getByTestId('user-library-total')).toHaveTextContent(
        '100 favorite photos loaded',
      ),
    );
    for (let loaded = 100; loaded < total; loaded += 100) {
      fireEvent.click(screen.getByTestId('user-library-load-more'));
      await waitFor(() =>
        expect(screen.getByTestId('user-library-total')).toHaveTextContent(
          String(Math.min(loaded + 100, total)),
        ),
      );
    }
    fireEvent.click(screen.getByTestId('user-library-select-mode'));
    fireEvent.click(screen.getByTestId('user-library-select-visible'));

    expect(screen.getByTestId('user-library-selection-status')).toHaveTextContent('500');
    expect(screen.getByText('The tray holds up to 500 photos.')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('checkbox', { name: 'unselected@N00' }));

    expect(await screen.findByTestId('user-library-photo-600')).toBeInTheDocument();
    expect(screen.getByTestId('user-library-photo-select-600')).toBeDisabled();
    expect(screen.getByTestId('user-library-photo-tray-600')).toBeDisabled();
  }, 15_000);

  it('shows a recoverable initial error and retries the first page', async () => {
    let requests = 0;
    server.use(
      http.get('*/favorites', () => {
        requests += 1;
        return requests === 1 ? json({ title: 'unavailable' }, 503) : json({ items: [] });
      }),
    );
    open();

    expect(await screen.findByTestId('user-library-error')).toBeInTheDocument();
    fireEvent.click(screen.getByTestId('user-library-retry'));

    expect(await screen.findByTestId('user-library-empty')).toBeInTheDocument();
    expect(requests).toBe(2);
  });

  it('retries a failed cursor page without discarding loaded photos', async () => {
    let cursorRequests = 0;
    server.use(
      http.get('*/favorites', ({ request }) => {
        if (new URL(request.url).searchParams.has('cursor')) {
          cursorRequests += 1;
          return cursorRequests === 1
            ? json({ code: 'unavailable' }, 503)
            : json({ items: [favorite(32)] });
        }
        return json({ items: [favorite(31)], next_cursor: 'second' });
      }),
    );
    open();
    await screen.findByTestId('user-library-photo-31');
    fireEvent.click(screen.getByTestId('user-library-load-more'));

    expect(await screen.findByTestId('user-library-more-error')).toBeInTheDocument();
    expect(screen.getByTestId('user-library-photo-31')).toBeInTheDocument();
    fireEvent.click(screen.getByTestId('user-library-load-more-retry'));
    expect(await screen.findByTestId('user-library-photo-32')).toBeInTheDocument();
    expect(cursorRequests).toBe(2);
  });

  it('renders a placeholder and pending flags when a photo has no image size', async () => {
    server.use(
      http.get('*/favorites', () =>
        json({
          items: [
            favorite(41, 'no-image@N00', {
              title: ' ',
              sizes: [],
              sizes_status: 'none',
              ai_status: 'none',
            }),
            favorite(42, 'small@N00', {
              sizes: [{ label: 'Small', width: 160, height: 120, url: 'https://images.test/42' }],
            }),
          ],
        }),
      ),
    );
    open();

    expect(
      await screen.findByRole('img', { name: 'Untitled photo by no-image@N00' }),
    ).toBeInTheDocument();
    expect(screen.getByTestId('user-library-photo-sizes-41')).toHaveTextContent('Sizes pending');
    expect(screen.getByTestId('user-library-photo-captioned-41')).toHaveTextContent(
      'Caption pending',
    );
    expect(screen.getByRole('img', { name: 'Photo 42 by small@N00' })).toHaveAttribute(
      'src',
      'https://images.test/42',
    );
    fireEvent.click(screen.getByTestId('user-library-photo-tray-41'));
    expect(screen.getByTestId('user-library-photo-tray-41')).toHaveAttribute(
      'aria-pressed',
      'true',
    );
    fireEvent.click(screen.getByTestId('user-library-photo-tray-41'));
    expect(screen.getByTestId('user-library-photo-tray-41')).toHaveAttribute(
      'aria-pressed',
      'false',
    );
  });

  it('observes grid width changes and disconnects after unmount', async () => {
    let resize: ResizeObserverCallback | undefined;
    const disconnect = vi.fn();
    const observe = vi.fn((target: Element) => {
      resize?.(
        [{ target, contentRect: { width: 640 } } as ResizeObserverEntry],
        {} as ResizeObserver,
      );
    });
    class ResizeObserverStub {
      constructor(callback: ResizeObserverCallback) {
        resize = callback;
      }

      observe = observe;

      disconnect = disconnect;

      unobserve = vi.fn();
    }
    vi.stubGlobal('ResizeObserver', ResizeObserverStub);
    server.use(http.get('*/favorites', () => json({ items: [favorite(51)] })));
    const { unmount } = open();

    try {
      expect(await screen.findByTestId('user-library-photo-51')).toBeInTheDocument();
      expect(observe).toHaveBeenCalled();
    } finally {
      unmount();
      vi.unstubAllGlobals();
    }

    expect(disconnect).toHaveBeenCalled();
  });

  it('submits selected photos to the download tray and navigates on success', async () => {
    let submitted: unknown;
    server.use(
      http.get('*/favorites', () => json({ items: [favorite(7), favorite(8)] })),
      http.post('*/downloads', async ({ request }) => {
        submitted = await request.json();
        return json(
          {
            id: 44,
            kind: 'tray',
            subject: 'tray',
            status: 'queued',
            created_at: '2026-11-09T00:00:00Z',
            expires_at: '2026-11-09T01:00:00Z',
            item_count: 1,
            skipped: 0,
          },
          201,
        );
      }),
    );
    const { router } = open();
    await screen.findByTestId('user-library-photo-7');
    fireEvent.click(screen.getByTestId('user-library-select-mode'));
    fireEvent.click(screen.getByTestId('user-library-photo-select-7'));
    fireEvent.click(screen.getByTestId('user-library-download-tray'));

    await waitFor(() => expect(submitted).toEqual({ kind: 'tray', photo_ids: [7] }));
    await waitFor(() => expect(router.state.location.pathname).toBe('/downloads'));
    expect(router.state.location.search).toBe('?id=44');
  });

  it('renders localized copy and the required loading state', async () => {
    let release: () => void = () => undefined;
    const blocked = new Promise<void>((resolve) => {
      release = resolve;
    });
    server.use(
      http.get('*/favorites', async () => {
        await blocked;
        return json({ items: [] });
      }),
    );
    open('vi');

    expect(await screen.findByRole('heading', { level: 1, name: 'Thư viện' })).toBeInTheDocument();
    expect(screen.getByTestId('user-library-loading')).toHaveTextContent('Đang tải thư viện');
    release();
    expect(await screen.findByTestId('user-library-empty')).toBeInTheDocument();
  });

  it('shows forbidden access without a retry on 403', async () => {
    server.use(http.get('*/favorites', () => json({ code: 'forbidden' }, 403)));
    open();

    expect(await screen.findByTestId('user-library-forbidden')).toBeInTheDocument();
    expect(screen.queryByTestId('user-library-retry')).not.toBeInTheDocument();
  });

  it('keeps the grid and selection when a tray request fails', async () => {
    let submissions = 0;
    server.use(
      http.get('*/favorites', () => json({ items: [favorite(9)] })),
      http.post('*/downloads', () => {
        submissions += 1;
        return submissions === 1
          ? json({ code: 'temp_storage_full' }, 500)
          : json({ code: 'flickr_key_required' }, 500);
      }),
    );
    open();
    await screen.findByTestId('user-library-photo-9');
    fireEvent.click(screen.getByTestId('user-library-select-mode'));
    fireEvent.click(screen.getByTestId('user-library-photo-select-9'));
    fireEvent.click(screen.getByTestId('user-library-download-tray'));

    expect(await screen.findByTestId('user-library-action-error')).toHaveTextContent(
      'Temporary download storage is full.',
    );
    expect(screen.getByTestId('user-library-photo-select-9')).toBeChecked();
    expect(screen.getByTestId('user-library-photo-9')).toBeInTheDocument();
    expect(screen.getByTestId('user-library-action-error')).toHaveTextContent(
      'Temporary download storage is full.',
    );

    fireEvent.click(screen.getByTestId('user-library-download-tray'));
    expect(await screen.findByTestId('user-library-action-error')).toHaveTextContent(
      'Add or check a valid Flickr API key to download.',
    );
    expect(screen.getByTestId('user-library-action-error').querySelector('a')).toHaveAttribute(
      'href',
      '/keys',
    );
  });
});
