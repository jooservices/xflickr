import { fireEvent, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw/http';
import { beforeEach, describe, expect, it } from 'vitest';
import { server } from '../../test/server';
import { renderApp, userSession } from '../../test/render';

const json = (body: unknown) => HttpResponse.json(body as never);

beforeEach(() => server.resetHandlers());

describe('ContactPage', () => {
  it('updates the confirmed crawl state when pausing and resuming', async () => {
    let pauseCalls = 0;
    let resumeCalls = 0;
    server.use(
      http.get('*/me/contacts/one@N00', () =>
        json({
          nsid: 'one@N00',
          username: 'one',
          real_name: 'One Person',
          avatar_url: null,
          cover_photo_url: null,
          location: null,
          depth: 1,
          found_via: [],
          starred: false,
          paused: false,
          crawl_status: 'running',
          photo_records_crawled: 1,
          photo_records_total: 1,
          sizes_completed: 0,
          facets: [],
          counts: { photos: 1, favorites: 0, albums: 0, galleries: 0, groups: 0 },
          updated_at: null,
        }),
      ),
      http.get('*/me/contacts/one@N00/photos', () =>
        json({ items: [], total: 0, next_cursor: null, updated_at: null }),
      ),
      http.put('*/me/crawl/pause', async ({ request }) => {
        pauseCalls += 1;
        expect(await request.json()).toEqual({ nsid: 'one@N00' });
        return new HttpResponse(null, { status: 204 });
      }),
      http.put('*/me/crawl/resume', async ({ request }) => {
        resumeCalls += 1;
        expect(await request.json()).toEqual({ nsid: 'one@N00' });
        return new HttpResponse(null, { status: 204 });
      }),
    );

    renderApp({ session: userSession, path: '/contacts/one@N00' });
    const toggle = await screen.findByTestId('user-contact-photos-crawl-toggle');

    expect(toggle).toHaveTextContent('Pause crawl');
    fireEvent.click(toggle);
    await waitFor(() => expect(toggle).toHaveTextContent('Resume crawl'));
    expect(pauseCalls).toBe(1);
    expect(await screen.findByText('Contact crawl paused.')).toBeInTheDocument();

    fireEvent.click(toggle);
    await waitFor(() => expect(toggle).toHaveTextContent('Pause crawl'));
    expect(resumeCalls).toBe(1);
    expect(await screen.findByText('Contact crawl resumed.')).toBeInTheDocument();
  });

  it('selects the current contact for priority', async () => {
    const sizePriorityCursors: (string | null)[] = [];
    server.use(
      http.get('*/me/contacts/one@N00', () =>
        json({
          nsid: 'one@N00',
          username: 'one',
          real_name: 'One Person',
          avatar_url: null,
          cover_photo_url: null,
          location: null,
          depth: 1,
          found_via: [],
          starred: false,
          paused: false,
          crawl_status: 'running',
          photo_records_crawled: 1,
          photo_records_total: 1,
          sizes_completed: 0,
          facets: [],
          counts: { photos: 1, favorites: 0, albums: 0, galleries: 0, groups: 0 },
          updated_at: null,
        }),
      ),
      http.get('*/me/contacts/one@N00/photos', () =>
        json({
          items: [
            {
              id: '11',
              title: 'Rainy street',
              thumbnail_url: null,
              width: null,
              height: null,
              taken_at: null,
              sizes_available: false,
            },
          ],
          total: 1,
          next_cursor: null,
          updated_at: null,
        }),
      ),
      http.put('*/me/contacts/one@N00/sizes-priority', ({ request }) => {
        const cursor = new URL(request.url).searchParams.get('cursor');
        sizePriorityCursors.push(cursor);
        return cursor === null
          ? json({ prioritised: 500, next_cursor: 'next-batch' })
          : json({ prioritised: 1, next_cursor: null });
      }),
      http.put('*/me/contacts/one@N00/select', () => new HttpResponse(null, { status: 204 })),
    );

    renderApp({ session: userSession, path: '/contacts/one@N00' });
    expect(await screen.findByText('Rainy street')).toBeInTheDocument();

    fireEvent.click(screen.getByTestId('user-contact-photos-prioritise-sizes'));
    await waitFor(() =>
      expect(screen.getByText('Prioritised 501 photo sizes.')).toBeInTheDocument(),
    );
    expect(sizePriorityCursors).toEqual([null, 'next-batch']);

    fireEvent.click(screen.getByTestId('user-contact-photos-prioritise-contact'));

    await waitFor(() =>
      expect(screen.getByText('Contact prioritised for seven days.')).toBeInTheDocument(),
    );
  });

  it('loads the next album page without losing the first page', async () => {
    server.use(
      http.get('*/me/contacts/one@N00', () =>
        json({
          nsid: 'one@N00',
          username: 'one',
          real_name: 'One Person',
          avatar_url: null,
          cover_photo_url: null,
          location: null,
          depth: 1,
          found_via: [],
          starred: false,
          paused: false,
          crawl_status: 'running',
          photo_records_crawled: 1,
          photo_records_total: 1,
          sizes_completed: 0,
          facets: [],
          counts: { photos: 1, favorites: 0, albums: 2, galleries: 0, groups: 0 },
          updated_at: null,
        }),
      ),
      http.get('*/me/contacts/one@N00/albums', ({ request }) => {
        const cursor = new URL(request.url).searchParams.get('cursor');
        const first = cursor === null;
        return json({
          items: [
            {
              id: first ? 'album-one' : 'album-two',
              title: first ? 'First album' : 'Second album',
              owner_name: 'one',
              stored_count: 1,
              total_count: 2,
              cover_photo_urls: [],
            },
          ],
          total: 2,
          next_cursor: first ? 'page-two' : null,
          updated_at: null,
        });
      }),
    );

    renderApp({ session: userSession, path: '/contacts/one@N00?tab=albums' });
    expect(await screen.findByText('First album')).toBeInTheDocument();

    fireEvent.click(screen.getByTestId('user-contact-albums-load-more'));

    expect(await screen.findByText('Second album')).toBeInTheDocument();
    expect(screen.getByText('First album')).toBeInTheDocument();
  });
});
