import { fireEvent, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw/http';
import { describe, expect, it } from 'vitest';
import { server } from '../../test/server';
import { renderApp, userSession } from '../../test/render';

const albums = {
  items: [
    {
      id: 'album-1',
      owner_nsid: 'owner-1',
      title: 'Vietnam 2024',
      updated_at: '2026-11-08T12:00:00Z',
    },
    {
      id: 'album-2',
      owner_nsid: 'owner-2',
      title: 'Night skies',
      updated_at: '2026-11-07T12:00:00Z',
    },
  ],
  contact_owners: ['owner-1', 'owner-2'],
  next_cursor: undefined,
};

const json = (body: unknown, status = 200) => HttpResponse.json(body as never, { status });

const open = (path = '/albums', locale: 'en' | 'vi' = 'en') =>
  renderApp({ session: userSession, path, locale });

describe('AlbumsPage', () => {
  it('shows paginated contact albums with an owner filter and contact query state', async () => {
    let requestedCursor: string | null = null;
    let requestedPath = '';
    let requestedOwner = '';
    server.use(
      http.get('*/me/albums', ({ request }) => {
        const url = new URL(request.url);
        requestedPath = url.pathname;
        requestedCursor = url.searchParams.get('cursor');
        const owner = url.searchParams.get('owner_nsid');
        requestedOwner = owner ?? '';
        return json({
          ...albums,
          items: owner ? albums.items.filter((album) => album.owner_nsid === owner) : albums.items,
        });
      }),
    );

    const { router } = open();

    expect(await screen.findByRole('heading', { level: 1, name: 'Albums' })).toBeInTheDocument();
    expect(document.title).toBe('Albums · XFlickr');
    expect(await screen.findAllByTestId('album-card')).toHaveLength(2);
    expect(requestedPath).toBe('/me/albums');
    expect(screen.getAllByTestId('album-card')[0]).toHaveAttribute('data-album-id', 'album-1');
    expect(screen.getAllByTestId('album-card')[0]).toHaveTextContent('Vietnam 2024');
    expect(screen.getAllByTestId('album-card')[0]).toHaveTextContent('owner-1');
    expect(screen.getAllByTestId('album-card')[0]).toHaveTextContent('Nov 8, 2026');
    expect(screen.getAllByTestId('album-card')[0]).not.toHaveTextContent(
      /visible photo|stored of/i,
    );
    expect(screen.getAllByTestId('album-card')[0]).not.toHaveAttribute('href');
    expect(screen.getByTestId('albums-filter')).toHaveValue('');
    expect(requestedCursor).toBeNull();

    fireEvent.change(screen.getByTestId('albums-filter'), { target: { value: 'owner-2' } });
    await waitFor(() => expect(router.state.location.search).toBe('?contact=owner-2'));
    await waitFor(() => expect(requestedOwner).toBe('owner-2'));
    await waitFor(() => expect(screen.getAllByTestId('album-card')).toHaveLength(1));
    expect(screen.getByTestId('album-card')).toHaveAttribute('data-album-id', 'album-2');
    expect(screen.getByTestId('album-card')).toHaveTextContent('Night skies');
    expect(screen.getByTestId('album-card')).not.toHaveTextContent(/visible photo|stored of/i);
  });

  it('offers all contact owners and filters albums on the server', async () => {
    let requestedOwner = '';
    server.use(
      http.get('*/me/albums', ({ request }) => {
        const url = new URL(request.url);
        requestedOwner = url.searchParams.get('owner_nsid') ?? '';
        return requestedOwner === 'owner-2'
          ? json({ items: [albums.items[1]], contact_owners: ['owner-1', 'owner-2'] })
          : json({
              items: [albums.items[0]],
              contact_owners: ['owner-1', 'owner-2'],
              next_cursor: 'next',
            });
      }),
    );

    const { router } = open();

    expect(await screen.findByTestId('album-card')).toHaveAttribute('data-album-id', 'album-1');
    expect(screen.getByRole('option', { name: 'owner-2' })).toBeInTheDocument();
    fireEvent.change(screen.getByTestId('albums-filter'), { target: { value: 'owner-2' } });

    await waitFor(() => expect(router.state.location.search).toBe('?contact=owner-2'));
    await waitFor(() => expect(requestedOwner).toBe('owner-2'));
    expect(await screen.findByTestId('album-card')).toHaveAttribute('data-album-id', 'album-2');
  });

  it('loads the next keyset page without hiding albums already on screen', async () => {
    let calls = 0;
    server.use(
      http.get('*/me/albums', ({ request }) => {
        calls += 1;
        const cursor = new URL(request.url).searchParams.get('cursor');
        return cursor === 'next'
          ? json({ items: [{ ...albums.items[1], id: 'album-3' }] })
          : json({ ...albums, next_cursor: 'next' });
      }),
    );
    open();

    expect(await screen.findAllByTestId('album-card')).toHaveLength(2);
    fireEvent.click(screen.getByTestId('albums-load-more'));

    await waitFor(() => expect(screen.getAllByTestId('album-card')).toHaveLength(3));
    expect(calls).toBe(2);
  });

  it('shows the localized successful empty state', async () => {
    server.use(http.get('*/me/albums', () => json({ items: [] })));
    open('/albums', 'vi');

    expect(await screen.findByTestId('albums-empty')).toHaveTextContent('Chưa có album');
    expect(screen.queryByTestId('albums-empty-action')).not.toBeInTheDocument();
    expect(screen.getByTestId('albums-empty').querySelector('a')).toBeNull();
    expect(screen.queryByTestId('albums-error')).not.toBeInTheDocument();
  });

  it('shows a retryable error without treating a failed request as empty', async () => {
    server.use(http.get('*/me/albums', () => json({ code: 'internal_error' }, 500)));
    open();

    expect(await screen.findByTestId('albums-error')).toHaveTextContent('Could not load albums');
    expect(screen.getByTestId('albums-retry')).toBeEnabled();
    expect(screen.queryByTestId('albums-empty')).not.toBeInTheDocument();
  });

  it('retries an initial error and renders albums when the request recovers', async () => {
    let calls = 0;
    server.use(
      http.get('*/me/albums', () => {
        calls += 1;
        return calls === 1 ? json({ code: 'internal_error' }, 500) : json(albums);
      }),
    );
    open();

    fireEvent.click(await screen.findByTestId('albums-retry'));
    expect(await screen.findAllByTestId('album-card')).toHaveLength(2);
    expect(calls).toBe(2);
  });

  it('resets an unknown contact filter to All and shows the inline notice', async () => {
    server.use(http.get('*/me/albums', () => json(albums)));
    const { router } = open('/albums?contact=missing-owner');
    expect(
      await screen.findByText('This contact is unavailable. Showing all contacts.'),
    ).toBeInTheDocument();
    await waitFor(() => expect(router.state.location.search).toBe(''));
    expect(screen.getByTestId('albums-filter')).toHaveValue('');
    expect(await screen.findAllByTestId('album-card')).toHaveLength(2);
  });

  it('keeps a contact query until keyset pages establish it is unavailable', async () => {
    let requestedOwner = '';
    server.use(
      http.get('*/me/albums', ({ request }) => {
        requestedOwner = new URL(request.url).searchParams.get('owner_nsid') ?? '';
        return json({ items: [albums.items[1]], contact_owners: ['owner-1', 'owner-2'] });
      }),
    );
    const { router } = open('/albums?contact=owner-2');

    expect(await screen.findByTestId('album-card')).toHaveAttribute('data-album-id', 'album-2');
    expect(screen.getByTestId('albums-filter')).toHaveValue('owner-2');
    expect(router.state.location.search).toBe('?contact=owner-2');
    await waitFor(() => expect(requestedOwner).toBe('owner-2'));
  });

  it('shows the delayed skeleton during the initial request', async () => {
    let finish: (() => void) | undefined;
    server.use(
      http.get('*/me/albums', async () => {
        await new Promise<void>((resolve) => {
          finish = () => resolve();
        });
        return json(albums);
      }),
    );
    open();

    expect(await screen.findByTestId('albums-loading')).toBeInTheDocument();
    finish?.();
    expect(await screen.findAllByTestId('album-card')).toHaveLength(2);
  });

  it('retains albums and marks a failed next-page request as partial', async () => {
    let calls = 0;
    server.use(
      http.get('*/me/albums', () => {
        calls += 1;
        if (calls === 1) return json({ ...albums, next_cursor: 'next' });
        if (calls === 2) return json({ code: 'internal_error' }, 500);
        return json({ items: [{ ...albums.items[1], id: 'album-3' }] });
      }),
    );
    open();

    expect(await screen.findAllByTestId('album-card')).toHaveLength(2);
    fireEvent.click(screen.getByTestId('albums-load-more'));

    expect(await screen.findByTestId('albums-partial')).toHaveTextContent(
      'Albums may be out of date',
    );
    expect(screen.getAllByTestId('album-card')).toHaveLength(2);
    expect(screen.getByTestId('albums-retry')).toBeEnabled();
    fireEvent.click(screen.getByTestId('albums-retry'));
    await waitFor(() => expect(screen.getAllByTestId('album-card')).toHaveLength(3));
    expect(screen.queryByTestId('albums-partial')).not.toBeInTheDocument();
  });

  it('shows the shared forbidden state for a forbidden catalog response', async () => {
    server.use(http.get('*/me/albums', () => json({ code: 'forbidden' }, 403)));
    open();

    expect(await screen.findByTestId('albums-forbidden')).toHaveTextContent(
      'You do not have access to this page',
    );
    expect(screen.queryByTestId('album-card')).not.toBeInTheDocument();
  });
});
