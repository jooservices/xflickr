import { fireEvent, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw/http';
import { afterEach, describe, expect, it, vi } from 'vitest';
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
  sizes: [
    { label: 'Medium', width: 320, height: 240, source: `https://images.test/${String(id)}` },
  ],
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

afterEach(() => {
  window.sessionStorage.clear();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe('LibraryPage', () => {
  it('opens the selected photo in gallery order and renders available catalog details', async () => {
    server.use(
      http.get('*/favorites', () => json({ items: [favorite(1), favorite(2)] })),
      http.get('*/photos/:id', ({ params }) =>
        json(
          photo(Number(params.id), 'linh@N00', {
            title: 'Aurora',
            description: 'Northern lights',
            labels: ['night-sky'],
            sizes: [
              {
                label: 'Medium',
                width: 800,
                height: 600,
                source: 'https://images.test/aurora',
              },
            ],
          }),
        ),
      ),
    );
    const { router } = open();

    await screen.findByTestId('user-library-photo-2');
    fireEvent.click(screen.getByTestId('user-library-photo-open-2'));

    const viewer = await screen.findByRole('dialog', { name: 'Aurora' });
    expect(screen.getByTestId('lightbox-position')).toHaveTextContent('Photo 2 of 2');
    expect(screen.getByRole('link', { name: 'linh@N00' })).toHaveAttribute(
      'href',
      '/contacts/linh%40N00',
    );
    expect(viewer).toHaveTextContent('Northern lights');
    expect(viewer).toHaveTextContent('night-sky');
    expect(viewer).toHaveTextContent('800 × 600');
    expect(screen.getByRole('link', { name: 'night-sky' })).toHaveAttribute(
      'href',
      '/search?q=night-sky',
    );

    fireEvent.click(screen.getByRole('link', { name: 'linh@N00' }));
    await waitFor(() => expect(screen.queryByTestId('photo-lightbox')).toBeNull());
    expect(router.state.location.pathname).toBe('/contacts/linh%40N00');
  });

  it('closes the viewer and opens the search route from a tag', async () => {
    server.use(
      http.get('*/favorites', () => json({ items: [favorite(1)] })),
      http.get('*/photos/:id', () => json(photo(1, 'linh@N00', { labels: ['night-sky'] }))),
    );
    const { router } = open();

    await screen.findByTestId('user-library-photo-1');
    fireEvent.click(screen.getByTestId('user-library-photo-open-1'));
    await screen.findByRole('dialog', { name: 'Photo 1' });
    fireEvent.click(screen.getByRole('link', { name: 'night-sky' }));

    await waitFor(() => expect(screen.queryByTestId('photo-lightbox')).toBeNull());
    expect(router.state.location.pathname).toBe('/search');
    expect(router.state.location.search).toBe('?q=night-sky');
  });

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

  it('uses singular copy when one favorite photo is loaded', async () => {
    server.use(http.get('*/favorites', () => json({ items: [favorite(1)] })));
    open();

    await waitFor(() =>
      expect(screen.getByTestId('user-library-total')).toHaveTextContent('1 favorite photo loaded'),
    );
  });

  it('reveals the photo caption when its tile receives keyboard focus', async () => {
    server.use(http.get('*/favorites', () => json({ items: [favorite(1)] })));
    open();
    const tile = await screen.findByTestId('user-library-photo-open-1');

    expect(tile).toHaveAccessibleName('Open Photo 1');
    tile.focus();
    expect(document.activeElement).toBe(tile);
    expect(screen.getByTestId('user-library-photo-caption-1')).toBeInTheDocument();
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

  it('selects remaining visible photos when one is already selected', async () => {
    let favoritesRequests = 0;
    let downloadRequests = 0;
    server.use(
      http.get('*/favorites', () => {
        favoritesRequests += 1;
        return json({ items: [favorite(21), favorite(22), favorite(23)] });
      }),
      http.post('*/downloads', () => {
        downloadRequests += 1;
        return json({}, 201);
      }),
    );
    open();
    await screen.findByTestId('user-library-photo-21');
    fireEvent.click(screen.getByTestId('user-library-select-mode'));
    fireEvent.click(screen.getByTestId('user-library-photo-select-21'));
    fireEvent.click(screen.getByTestId('user-library-select-visible'));

    expect(screen.getByTestId('user-library-selection-status')).toHaveTextContent('3');
    expect(screen.getByTestId('user-library-photo-select-21')).toBeChecked();
    expect(screen.getByTestId('user-library-photo-select-22')).toBeChecked();
    expect(screen.getByTestId('user-library-photo-select-23')).toBeChecked();
    expect(favoritesRequests).toBe(1);
    expect(downloadRequests).toBe(0);
  });

  it('fills the last selection slot with the first new photo', async () => {
    const total = 501;
    server.use(
      http.get('*/favorites', ({ request }) => {
        const start = Number(new URL(request.url).searchParams.get('cursor') ?? '0');
        const end = Math.min(start + 100, total);
        const items = Array.from({ length: end - start }, (_, index) => {
          const offset = start + index;
          const owner = offset < 497 ? 'bulk@N00' : offset < 499 ? 'pair@N00' : 'new@N00';
          const sizesStatus = offset < 497 ? 'none' : 'confirmed';
          return favorite(offset + 1, owner, { sizes_status: sizesStatus });
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
    fireEvent.click(screen.getByRole('checkbox', { name: 'bulk@N00' }));
    fireEvent.click(screen.getByTestId('user-library-select-visible'));
    expect(screen.getByTestId('user-library-selection-status')).toHaveTextContent('497');

    fireEvent.click(screen.getByTestId('user-library-filter-clear'));
    fireEvent.click(screen.getByRole('checkbox', { name: 'pair@N00' }));
    fireEvent.click(screen.getByTestId('user-library-select-visible'));
    expect(screen.getByTestId('user-library-selection-status')).toHaveTextContent('499');

    fireEvent.click(screen.getByTestId('user-library-filter-clear'));
    fireEvent.click(screen.getByTestId('user-library-filter-sizes'));
    fireEvent.click(screen.getByTestId('user-library-select-visible'));

    expect(screen.getByTestId('user-library-selection-status')).toHaveTextContent('500');
    expect(
      screen.getByText(
        'The selection limit is 500 photos. Selected photos have not been added to the download tray.',
      ),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByTestId('shell-lang-toggle'));
    expect(
      screen.getByText(
        'Giới hạn lựa chọn là 500 ảnh. Các ảnh đã chọn chưa được thêm vào danh sách tải.',
      ),
    ).toBeInTheDocument();
    expect(screen.getByTestId('user-library-photo-select-500')).toBeChecked();
    expect(screen.getByTestId('user-library-photo-select-501')).not.toBeChecked();
    expect(screen.queryByTestId('user-library-photo-tray-501')).not.toBeInTheDocument();
  }, 15_000);

  it('preserves selection across filters and Escape, then clears the entire selection', async () => {
    let favoritesRequests = 0;
    server.use(
      http.get('*/favorites', () => {
        favoritesRequests += 1;
        return json({ items: [favorite(1), favorite(2, 'morgan@N00'), favorite(3, 'morgan@N00')] });
      }),
    );
    open();
    await screen.findByTestId('user-library-photo-1');
    fireEvent.click(screen.getByTestId('user-library-select-mode'));
    fireEvent.click(screen.getByTestId('user-library-photo-select-1'));
    fireEvent.click(screen.getByRole('checkbox', { name: 'morgan@N00' }));
    fireEvent.click(screen.getByTestId('user-library-select-visible'));

    expect(screen.getByTestId('user-library-selection-status')).toHaveTextContent('3');
    expect(screen.getByTestId('user-library-photo-select-2')).toBeChecked();
    expect(screen.getByTestId('user-library-photo-select-3')).toBeChecked();
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.getByTestId('user-library-select-mode')).toHaveAttribute('aria-pressed', 'false');

    fireEvent.click(screen.getByTestId('user-library-select-mode'));
    expect(screen.getByTestId('user-library-selection-status')).toHaveTextContent('3');
    expect(screen.getByTestId('user-library-photo-select-2')).toBeChecked();
    fireEvent.click(screen.getByTestId('user-library-photo-select-2'));
    expect(screen.getByTestId('user-library-photo-select-2')).not.toBeChecked();
    expect(screen.getByTestId('user-library-selection-status')).toHaveTextContent('2');

    fireEvent.click(screen.getByTestId('user-library-clear-selection'));
    expect(screen.getByTestId('user-library-selection-status')).toHaveTextContent('0');
    fireEvent.click(screen.getByTestId('user-library-filter-clear'));
    expect(screen.getByTestId('user-library-photo-select-1')).not.toBeChecked();
    expect(favoritesRequests).toBe(1);
  });

  it('keeps the selection when the user leaves the Library and opens it again', async () => {
    window.sessionStorage.clear();
    server.use(
      http.get('*/favorites', () => json({ items: [favorite(1), favorite(2, 'morgan@N00')] })),
    );
    const first = open();
    await screen.findByTestId('user-library-photo-1');
    fireEvent.click(screen.getByTestId('user-library-select-mode'));
    fireEvent.click(screen.getByTestId('user-library-photo-select-1'));
    fireEvent.click(screen.getByTestId('user-library-photo-select-2'));
    expect(screen.getByTestId('user-library-selection-status')).toHaveTextContent('2');
    first.unmount();

    open();
    await screen.findByTestId('user-library-photo-1');
    fireEvent.click(screen.getByTestId('user-library-select-mode'));
    expect(screen.getByTestId('user-library-selection-status')).toHaveTextContent('2');
    expect(screen.getByTestId('user-library-photo-select-1')).toBeChecked();
    fireEvent.click(screen.getByTestId('user-library-clear-selection'));
    expect(window.sessionStorage.getItem('xflickr.session.library.selection.2')).toBeNull();
  });

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

  it('keeps loaded photos and offers retry after a background refresh fails', async () => {
    let requests = 0;
    server.use(
      http.get('*/favorites', () => {
        requests += 1;
        return requests === 1
          ? json({ items: [favorite(51)] })
          : requests === 2
            ? json({ code: 'unavailable' }, 503)
            : json({ items: [favorite(51)] });
      }),
    );
    const app = open();
    await screen.findByTestId('user-library-photo-51');

    await app.client.refetchQueries({ queryKey: ['library', 'favorites'] });
    expect(await screen.findByTestId('user-library-stale')).toBeInTheDocument();
    expect(screen.getByTestId('user-library-photo-51')).toBeInTheDocument();
    fireEvent.click(screen.getByTestId('user-library-stale-retry'));

    await waitFor(() => expect(screen.queryByTestId('user-library-stale')).not.toBeInTheDocument());
    expect(requests).toBe(3);
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
              sizes: [
                { label: 'Small', width: 160, height: 120, source: 'https://images.test/42' },
              ],
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
    expect(screen.queryByTestId('user-library-photo-tray-41')).not.toBeInTheDocument();
  });

  it('reads the image link from source when confirmed and from url when provisional', async () => {
    server.use(
      http.get('*/favorites', () =>
        json({
          items: [
            favorite(51, 'confirmed@N00', {
              sizes: [
                {
                  label: 'Medium',
                  width: 320,
                  height: 240,
                  source: 'https://images.test/source-51',
                  url: 'https://flickr.test/page-51',
                },
              ],
            }),
            favorite(52, 'provisional@N00', {
              sizes_status: 'provisional',
              sizes: [
                {
                  label: 'Medium',
                  width: 320,
                  height: 240,
                  source: 'https://images.test/ignored-52',
                  url: 'https://images.test/url-52',
                },
              ],
            }),
          ],
        }),
      ),
    );
    open();

    expect(await screen.findByRole('img', { name: 'Photo 51 by confirmed@N00' })).toHaveAttribute(
      'src',
      'https://images.test/source-51',
    );
    expect(screen.getByRole('img', { name: 'Photo 52 by provisional@N00' })).toHaveAttribute(
      'src',
      'https://images.test/url-52',
    );
  });

  it('locks load more and retry while a background refetch is running', async () => {
    let release: (() => void) | undefined;
    let firstPageRequests = 0;
    server.use(
      http.get('*/favorites', async ({ request }) => {
        if (new URL(request.url).searchParams.has('cursor')) return json({ items: [favorite(62)] });
        firstPageRequests += 1;
        if (firstPageRequests > 1) {
          await new Promise<void>((resolve) => {
            release = resolve;
          });
        }
        return json({ items: [favorite(61)], next_cursor: 'second' });
      }),
    );
    const app = open();
    await screen.findByTestId('user-library-photo-61');
    void app.client.refetchQueries({ queryKey: ['library', 'favorites'] });
    await waitFor(() => expect(firstPageRequests).toBeGreaterThan(1));
    expect(screen.getByTestId('user-library-load-more')).toBeDisabled();
    release?.();
    await waitFor(() => expect(screen.getByTestId('user-library-load-more')).toBeEnabled());
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

  it('re-measures the grid offset when content above it changes height', async () => {
    const callbacks: ResizeObserverCallback[] = [];
    const observed: Element[] = [];
    class ResizeObserverStub {
      constructor(callback: ResizeObserverCallback) {
        callbacks.push(callback);
      }

      observe = vi.fn((target: Element) => {
        observed.push(target);
      });

      disconnect = vi.fn();

      unobserve = vi.fn();
    }
    vi.stubGlobal('ResizeObserver', ResizeObserverStub);
    let top = 100;
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(
      () =>
        ({ top, bottom: top, left: 0, right: 0, width: 800, height: 0, x: 0, y: top }) as DOMRect,
    );
    server.use(http.get('*/favorites', () => json({ items: [favorite(61), favorite(62)] })));
    open();

    const grid = await screen.findByTestId('user-library-grid');
    expect(grid).toHaveAttribute('data-scroll-margin', '100');
    // The element rendered right before the grid (the results bar) is watched, not only <body>.
    const before = grid.previousElementSibling ?? grid.parentElement?.previousElementSibling;
    expect(before).not.toBeNull();
    expect(observed).toContain(before);
    top = 260;
    for (const callback of callbacks) {
      callback([], {} as ResizeObserver);
    }
    await waitFor(() => expect(grid).toHaveAttribute('data-scroll-margin', '260'));
  });

  it('hides download navigation and does not request downloads for a selection', async () => {
    let downloadRequests = 0;
    server.use(
      http.get('*/favorites', () => json({ items: [favorite(7)] })),
      http.post('*/downloads', () => {
        downloadRequests += 1;
        return json({}, 201);
      }),
    );
    const { router } = open();
    await screen.findByTestId('user-library-photo-7');
    fireEvent.click(screen.getByTestId('user-library-select-mode'));
    fireEvent.click(screen.getByTestId('user-library-photo-select-7'));

    expect(screen.getByTestId('user-library-selection-status')).toHaveTextContent('1');
    expect(screen.queryByTestId('user-library-photo-tray-7')).not.toBeInTheDocument();
    expect(screen.queryByTestId('user-library-download-tray')).not.toBeInTheDocument();
    expect(router.state.location.pathname).toBe('/library');
    expect(downloadRequests).toBe(0);
  });

  it('does not link to the accounts placeholder from the empty state', async () => {
    server.use(http.get('*/favorites', () => json({ items: [] })));
    open();

    expect(await screen.findByTestId('user-library-empty')).toBeInTheDocument();
    expect(document.querySelector('a[href="/accounts"]')).toBeNull();
  });

  it.each([
    {
      locale: 'en' as const,
      title: 'No favorite photos yet',
      text: 'Photos you mark as favorites on Flickr appear here.',
    },
    {
      locale: 'vi' as const,
      title: 'Chưa có ảnh yêu thích',
      text: 'Ảnh bạn đánh dấu yêu thích trên Flickr sẽ xuất hiện tại đây.',
    },
  ])('describes an empty favorites list in $locale', async ({ locale, title, text }) => {
    server.use(http.get('*/favorites', () => json({ items: [] })));
    open(locale);

    expect(await screen.findByRole('heading', { level: 2, name: title })).toBeInTheDocument();
    expect(screen.getByText(text)).toBeInTheDocument();
  });

  it('waits for the final cursor page before showing the library empty state', async () => {
    server.use(
      http.get('*/favorites', ({ request }) => {
        const cursor = new URL(request.url).searchParams.get('cursor');
        return cursor ? json({ items: [] }) : json({ items: [], next_cursor: 'page-two' });
      }),
    );
    open();

    expect(await screen.findByTestId('user-library-load-more')).toBeInTheDocument();
    expect(screen.queryByTestId('user-library-empty')).not.toBeInTheDocument();

    fireEvent.click(screen.getByTestId('user-library-load-more'));
    expect(await screen.findByTestId('user-library-empty')).toBeInTheDocument();
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
});
