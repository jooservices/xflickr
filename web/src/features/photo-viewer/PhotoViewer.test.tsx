import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { useState } from 'react';
import { http, HttpResponse } from 'msw/http';
import { describe, expect, it, vi } from 'vitest';
import type { CatalogPhoto } from '../../shared/api/gen/models';
import { I18nProvider } from '../../shared/i18n/I18nProvider';
import type { Locale } from '../../shared/i18n';
import { SESSION_EXPIRED_EVENT } from '../../shared/api/sessionEvents';
import { server } from '../../test/server';
import { PhotoViewer } from './PhotoViewer';

const PHOTO_URL = 'https://images.example.test/photo.jpg';

function photo(id: number, overrides: Partial<CatalogPhoto> = {}): CatalogPhoto {
  return {
    id,
    owner_nsid: 'owner@example',
    title: `Photo ${String(id)}`,
    description: '',
    labels: [],
    visibility: 'public',
    sizes_status: 'provisional',
    sizes: [{ label: 'Large', url: PHOTO_URL, media: 'photo' }],
    ai_status: 'none',
    ...overrides,
  };
}

function jsonError(code: string, status: number) {
  return HttpResponse.json({ code, message: 'Private server detail' }, { status });
}

type HarnessProps = {
  photoIds: number[];
  initialIndex: number;
  locale: Locale;
  onClose: () => void;
};

function ViewerHarness({ photoIds, initialIndex, locale, onClose }: HarnessProps) {
  const [open, setOpen] = useState(false);
  const [client] = useState(
    () => new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } }),
  );
  const [opener, setOpener] = useState<HTMLButtonElement | null>(null);

  return (
    <I18nProvider initialLocale={locale}>
      <QueryClientProvider client={client}>
        <main>
          <h1 tabIndex={-1}>Gallery</h1>
          <button
            ref={setOpener}
            type="button"
            onClick={() => setOpen(true)}
            data-testid="gallery-opener"
          >
            Open photo
          </button>
          {open ? (
            <PhotoViewer
              photoIds={photoIds}
              initialIndex={initialIndex}
              opener={opener}
              onClose={() => {
                onClose();
                setOpen(false);
              }}
            />
          ) : null}
        </main>
      </QueryClientProvider>
    </I18nProvider>
  );
}

function openViewer(photoIds: number[] = [101, 202], initialIndex = 0, locale: Locale = 'en') {
  const onClose = vi.fn();
  render(
    <ViewerHarness
      photoIds={photoIds}
      initialIndex={initialIndex}
      locale={locale}
      onClose={onClose}
    />,
  );
  const opener = screen.getByTestId('gallery-opener');
  fireEvent.click(opener);
  return { onClose, opener };
}

describe('PhotoViewer', () => {
  it.each([
    { name: 'an empty list', photoIds: [], initialIndex: 0 },
    { name: 'an index after the list', photoIds: [101], initialIndex: 1 },
    { name: 'a negative index', photoIds: [101], initialIndex: -1 },
  ])(
    'shows the empty state for $name without requesting a photo',
    async ({ photoIds, initialIndex }) => {
      const request = vi.fn();
      server.use(
        http.get('*/photos/:id', () => {
          request();
          return HttpResponse.json(photo(101));
        }),
      );
      const { onClose } = openViewer(photoIds, initialIndex);

      expect(await screen.findByTestId('lightbox-empty')).toHaveTextContent('No photo to show');
      expect(screen.getByTestId('lightbox-empty-action')).toHaveTextContent('Return to gallery');
      expect(request).not.toHaveBeenCalled();
      fireEvent.click(screen.getByTestId('lightbox-empty-action'));
      expect(onClose).toHaveBeenCalledOnce();
    },
  );

  it('shows loading while the current photo request is pending', async () => {
    let release!: () => void;
    const pending = new Promise<void>((resolve) => {
      release = resolve;
    });
    server.use(
      http.get('*/photos/:id', async () => {
        await pending;
        return HttpResponse.json(photo(101));
      }),
    );
    openViewer();

    expect(screen.getByTestId('lightbox-loading')).toHaveAttribute('aria-busy', 'true');
    expect(screen.getByTestId('lightbox-loading')).toHaveTextContent('Loading photo');
    release();
    expect(await screen.findByRole('img', { name: 'Photo 101' })).toHaveAttribute('src', PHOTO_URL);
  });

  it('requests only the selected photo once and uses its first valid photo URL', async () => {
    const requested: string[] = [];
    server.use(
      http.get('*/photos/:id', ({ params }) => {
        requested.push(String(params.id));
        return HttpResponse.json(
          photo(Number(params.id), {
            title: 'Aurora',
            sizes: [
              { label: 'Missing', url: '  ', media: 'photo' },
              { label: 'Video', url: 'https://images.example.test/video.mp4', media: 'video' },
              { label: 'Large', url: PHOTO_URL, media: 'photo' },
              { label: 'Original', url: 'https://images.example.test/original.jpg' },
            ],
          }),
        );
      }),
    );
    openViewer();

    expect(await screen.findByRole('img', { name: 'Aurora' })).toHaveAttribute('src', PHOTO_URL);
    expect(screen.getByRole('heading', { name: 'Aurora' })).toBeInTheDocument();
    expect(requested).toEqual(['101']);
    expect(screen.getByTestId('lightbox-position')).toHaveTextContent('Photo 1 of 2');
  });

  it('accepts a recorded size URL when its media field is absent', async () => {
    server.use(
      http.get('*/photos/:id', () =>
        HttpResponse.json(photo(101, { sizes: [{ label: 'Original', url: PHOTO_URL }] })),
      ),
    );
    openViewer([101]);

    expect(await screen.findByRole('img', { name: 'Photo 101' })).toHaveAttribute('src', PHOTO_URL);
  });

  it('uses the translated viewer name for an empty title and reports an unavailable preview', async () => {
    server.use(
      http.get('*/photos/:id', () =>
        HttpResponse.json(photo(101, { title: '', sizes: [{ label: 'Original' }] })),
      ),
    );
    openViewer([101], 0, 'vi');

    expect(await screen.findByTestId('lightbox-preview-error')).toHaveTextContent(
      'Không thể xem trước ảnh',
    );
    expect(screen.getByRole('heading', { name: 'Trình xem ảnh' })).toBeInTheDocument();
    expect(screen.queryByTestId('lightbox-image')).toBeNull();
    expect(screen.getByTestId('lightbox-retry')).toHaveTextContent('Thử lại');
  });

  it('uses the translated viewer name as the image alt when the title is empty', async () => {
    server.use(http.get('*/photos/:id', () => HttpResponse.json(photo(101, { title: '' }))));
    openViewer([101], 0, 'vi');

    expect(await screen.findByRole('img', { name: 'Trình xem ảnh' })).toHaveAttribute(
      'src',
      PHOTO_URL,
    );
  });

  it('replaces a failed image with a retryable preview message', async () => {
    server.use(http.get('*/photos/:id', () => HttpResponse.json(photo(101))));
    openViewer([101]);

    const image = await screen.findByRole('img', { name: 'Photo 101' });
    fireEvent.error(image);
    expect(screen.getByTestId('lightbox-stage')).toHaveTextContent('Photo preview unavailable');
    expect(screen.getByRole('heading', { name: 'Photo 101' })).toBeInTheDocument();
    fireEvent.click(screen.getByTestId('lightbox-retry'));
    expect(await screen.findByRole('img', { name: 'Photo 101' })).toBeInTheDocument();
  });

  it('shows a retryable server error without exposing the server message, then recovers', async () => {
    let calls = 0;
    server.use(
      http.get('*/photos/:id', () => {
        calls += 1;
        return calls === 1 ? jsonError('server_error', 500) : HttpResponse.json(photo(101));
      }),
    );
    openViewer([101]);

    expect(await screen.findByTestId('lightbox-error')).toHaveTextContent(
      'Could not load this photo',
    );
    expect(screen.getByTestId('lightbox-error')).toHaveTextContent(
      'Check your connection, then try again.',
    );
    expect(screen.queryByText('Private server detail')).toBeNull();
    fireEvent.click(screen.getByTestId('lightbox-retry'));
    expect(await screen.findByRole('img', { name: 'Photo 101' })).toBeInTheDocument();
    expect(calls).toBe(2);
  });

  it('shows a retryable error for a network failure', async () => {
    const requests = vi.fn();
    server.use(
      http.get('*/photos/:id', () => {
        requests();
        return HttpResponse.error();
      }),
    );
    openViewer([101]);

    expect(await screen.findByTestId('lightbox-error')).toHaveTextContent(
      'Could not load this photo',
    );
    expect(screen.getByTestId('lightbox-retry')).toBeEnabled();
    expect(requests).toHaveBeenCalledOnce();
  });

  it('clears the previous photo after a 404 and offers only gallery return', async () => {
    server.use(
      http.get('*/photos/:id', ({ params }) =>
        String(params.id) === '101' ? HttpResponse.json(photo(101)) : jsonError('not_found', 404),
      ),
    );
    const { onClose, opener } = openViewer();
    expect(await screen.findByRole('img', { name: 'Photo 101' })).toBeInTheDocument();

    fireEvent.keyDown(document, { key: 'ArrowRight' });
    expect(await screen.findByTestId('lightbox-not-found')).toHaveTextContent(
      'This photo is unavailable',
    );
    expect(screen.queryByRole('img', { name: 'Photo 101' })).toBeNull();
    expect(screen.queryByRole('heading', { name: 'Photo 101' })).toBeNull();
    expect(screen.queryByTestId('lightbox-retry')).toBeNull();
    expect(screen.getByTestId('lightbox-not-found-return')).toBeEnabled();
    fireEvent.click(screen.getByTestId('lightbox-not-found-return'));
    await waitFor(() => expect(screen.queryByTestId('photo-lightbox')).toBeNull());
    expect(onClose).toHaveBeenCalledOnce();
    expect(opener).toHaveFocus();
  });

  it('refetches when reopened and hides cached photo data until the response arrives', async () => {
    let calls = 0;
    server.use(
      http.get('*/photos/:id', () => {
        calls += 1;
        return calls === 1 ? HttpResponse.json(photo(101)) : jsonError('not_found', 404);
      }),
    );
    const { opener } = openViewer([101]);
    expect(await screen.findByRole('img', { name: 'Photo 101' })).toBeInTheDocument();
    fireEvent.click(screen.getByTestId('lightbox-close'));
    await waitFor(() => expect(screen.queryByTestId('photo-lightbox')).toBeNull());

    fireEvent.click(opener);
    expect(await screen.findByTestId('lightbox-loading')).toBeInTheDocument();
    expect(screen.queryByRole('img', { name: 'Photo 101' })).toBeNull();
    expect(await screen.findByTestId('lightbox-not-found')).toHaveTextContent(
      'This photo is unavailable',
    );
    expect(calls).toBe(2);
  });

  it('signals the shared session-expired flow and clears the previous photo after a 401', async () => {
    const expired = vi.fn();
    window.addEventListener(SESSION_EXPIRED_EVENT, expired);
    server.use(
      http.get('*/photos/:id', ({ params }) =>
        String(params.id) === '101'
          ? HttpResponse.json(photo(101))
          : jsonError('unauthenticated', 401),
      ),
    );
    openViewer();
    expect(await screen.findByRole('img', { name: 'Photo 101' })).toBeInTheDocument();

    try {
      fireEvent.keyDown(screen.getByTestId('lightbox-next'), { key: 'ArrowRight' });
      await waitFor(() => expect(expired).toHaveBeenCalledOnce());
      await waitFor(() => expect(screen.queryByRole('img', { name: 'Photo 101' })).toBeNull());
      expect(screen.queryByRole('heading', { name: 'Photo 101' })).toBeNull();
    } finally {
      window.removeEventListener(SESSION_EXPIRED_EVENT, expired);
    }
  });

  it('wraps with buttons and arrow keys while fetching only the current id', async () => {
    const requested: string[] = [];
    server.use(
      http.get('*/photos/:id', ({ params }) => {
        requested.push(String(params.id));
        return HttpResponse.json(photo(Number(params.id)));
      }),
    );
    openViewer();
    expect(await screen.findByRole('img', { name: 'Photo 101' })).toBeInTheDocument();

    fireEvent.click(screen.getByTestId('lightbox-next'));
    expect(await screen.findByRole('img', { name: 'Photo 202' })).toBeInTheDocument();
    expect(screen.getByTestId('lightbox-position')).toHaveTextContent('Photo 2 of 2');
    expect(requested).toEqual(['101', '202']);

    fireEvent.keyDown(document, { key: 'ArrowRight' });
    expect(await screen.findByRole('img', { name: 'Photo 101' })).toBeInTheDocument();
    expect(screen.getByTestId('lightbox-position')).toHaveTextContent('Photo 1 of 2');
    expect(requested.filter((id) => id === '202')).toEqual(['202']);

    fireEvent.click(screen.getByTestId('lightbox-previous'));
    expect(await screen.findByRole('img', { name: 'Photo 202' })).toBeInTheDocument();
    expect(screen.getByTestId('lightbox-position')).toHaveTextContent('Photo 2 of 2');

    fireEvent.keyDown(screen.getByTestId('lightbox-next'), { key: 'ArrowLeft' });
    expect(await screen.findByRole('img', { name: 'Photo 101' })).toBeInTheDocument();
    expect(screen.getByTestId('lightbox-position')).toHaveTextContent('Photo 1 of 2');
  });

  it('translates controls and position to Vietnamese', async () => {
    server.use(http.get('*/photos/:id', () => HttpResponse.json(photo(101))));
    openViewer([101, 202], 0, 'vi');

    const dialog = await screen.findByTestId('photo-lightbox');
    expect(within(dialog).getByRole('button', { name: 'Đóng trình xem ảnh' })).toBeInTheDocument();
    expect(screen.getByTestId('lightbox-position')).toHaveTextContent('Ảnh 1/2');
    expect(screen.getByTestId('lightbox-next')).toHaveAccessibleName('Ảnh tiếp theo');
  });

  it('focuses Close, traps Tab, closes with Escape and restores focus to the opener', async () => {
    server.use(http.get('*/photos/:id', () => HttpResponse.json(photo(101))));
    const { opener, onClose } = openViewer();
    const dialog = await screen.findByTestId('photo-lightbox');
    const close = screen.getByTestId('lightbox-close');
    const next = screen.getByTestId('lightbox-next');

    expect(close).toHaveFocus();
    fireEvent.keyDown(document, { key: 'Tab', shiftKey: true });
    expect(next).toHaveFocus();
    fireEvent.keyDown(document, { key: 'Tab' });
    expect(dialog).toContainElement(document.activeElement);

    fireEvent.keyDown(document, { key: 'Escape' });
    await waitFor(() => expect(screen.queryByTestId('photo-lightbox')).toBeNull());
    expect(onClose).toHaveBeenCalledOnce();
    expect(opener).toHaveFocus();
  });

  it('closes from the Close button and restores focus to its opener', async () => {
    server.use(http.get('*/photos/:id', () => HttpResponse.json(photo(101))));
    const { opener, onClose } = openViewer([101]);
    await screen.findByRole('img', { name: 'Photo 101' });

    fireEvent.click(screen.getByTestId('lightbox-close'));
    await waitFor(() => expect(screen.queryByTestId('photo-lightbox')).toBeNull());
    expect(onClose).toHaveBeenCalledOnce();
    expect(opener).toHaveFocus();
  });

  it('leaves Escape and arrow keys to a more recently opened dialog', async () => {
    server.use(http.get('*/photos/:id', () => HttpResponse.json(photo(101))));
    openViewer();
    await screen.findByRole('img', { name: 'Photo 101' });

    const otherDialog = document.createElement('div');
    otherDialog.setAttribute('role', 'alertdialog');
    otherDialog.setAttribute('aria-modal', 'true');
    const otherControl = document.createElement('button');
    otherControl.textContent = 'Top dialog';
    otherDialog.append(otherControl);
    document.body.append(otherDialog);
    try {
      otherControl.focus();
      fireEvent.keyDown(otherControl, { key: 'Escape' });
      fireEvent.keyDown(otherControl, { key: 'ArrowRight' });
      expect(screen.getByTestId('photo-lightbox')).toBeInTheDocument();
      expect(screen.getByRole('img', { name: 'Photo 101' })).toBeInTheDocument();
      expect(screen.getByTestId('lightbox-position')).toHaveTextContent('Photo 1 of 2');
    } finally {
      otherDialog.remove();
    }
  });

  it('returns focus to the gallery heading when the opener no longer exists', async () => {
    const { opener } = openViewer([], 0);
    const heading = screen.getByRole('heading', { name: 'Gallery' });
    opener.remove();

    fireEvent.click(await screen.findByTestId('lightbox-empty-action'));
    await waitFor(() => expect(screen.queryByTestId('photo-lightbox')).toBeNull());
    expect(heading).toHaveFocus();
  });
});
