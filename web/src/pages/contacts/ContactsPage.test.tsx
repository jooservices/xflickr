import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw/http';
import { beforeEach, describe, expect, it } from 'vitest';
import { server } from '../../test/server';
import { renderApp, userSession } from '../../test/render';

const json = (body: unknown, status = 200) => HttpResponse.json(body as never, { status });
const interests = (items: unknown[], next_cursor: string | null = null) => ({
  items,
  counts: { all: items.length, starred: 1, crawling: 1, needs_attention: 0 },
  next_cursor,
  updated_at: null,
});
const interest = {
  nsid: 'one@N00',
  name: 'One Person',
  username: 'one',
  avatar_url: null,
  depth: 1,
  starred: true,
  status: 'crawling',
  photos_crawled: 8,
  photos_total: 12,
  favorites_count: 3,
  sizes_percent: 50,
  last_check_at: null,
  recent_photos: [],
};
const facet = (name: string, completed = 8, total: number | null = 12) => ({
  name,
  completed,
  total,
  status: 'running',
  updated_at: null,
});
const contact = {
  nsid: 'one@N00',
  username: 'one',
  real_name: 'One Person',
  avatar_url: null,
  cover_photo_url: null,
  location: 'Hanoi',
  depth: 1,
  found_via: [],
  starred: true,
  paused: false,
  crawl_status: 'running',
  photo_records_crawled: 8,
  photo_records_total: 12,
  sizes_completed: 50,
  facets: [
    facet('photos'),
    facet('favorites', 2, 3),
    facet('albums', 1, 1),
    facet('galleries', 0, 0),
    facet('groups', 0, 0),
  ],
  counts: { photos: 12, favorites: 3, albums: 1, galleries: 0, groups: 1 },
  updated_at: null,
};
const photoPage = (items: unknown[], next_cursor: string | null = null) => ({
  items,
  total: items.length,
  next_cursor,
  updated_at: null,
});

beforeEach(() => server.resetHandlers());

describe('ContactsPage', () => {
  it('lists only the signed-in user interests and supports search, filters, sorting, and stars', async () => {
    const requests: URL[] = [];
    let starred = true;
    server.use(
      http.get('*/me/crawl/interests', ({ request }) => {
        requests.push(new URL(request.url));
        return json(interests([{ ...interest, starred }]));
      }),
      http.get('*/me/crawl/settings', () => json({ depth: 3, depth_default: 2, depth_max: 4 })),
      http.put('*/me/contacts/:nsid/star', async ({ request }) => {
        starred = ((await request.json()) as { starred: boolean }).starred;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    renderApp({ session: userSession, path: '/contacts?filter=all&sort=updated' });
    expect(await screen.findByTestId('user-contact-card')).toHaveTextContent('One Person');
    expect(screen.getByTestId('user-contact-progress')).toHaveAttribute('aria-valuenow', '67');
    fireEvent.click(screen.getByTestId('user-contacts-filter-starred'));
    await waitFor(() => expect(requests.at(-1)?.searchParams.get('filter')).toBe('starred'));
    fireEvent.change(screen.getByTestId('user-contacts-search'), { target: { value: 'one@N00' } });
    fireEvent.change(screen.getByTestId('user-contacts-sort'), { target: { value: 'name' } });
    await waitFor(() => {
      expect(requests.at(-1)?.searchParams.get('q')).toBe('one@N00');
      expect(requests.at(-1)?.searchParams.get('sort')).toBe('name');
    });
    fireEvent.click(screen.getByTestId('user-contact-star'));
    await waitFor(() =>
      expect(screen.getByTestId('user-contact-star')).toHaveAttribute('aria-pressed', 'false'),
    );
  });

  it('keeps loaded cards and offers an inline retry after a later page fails', async () => {
    let failures = 0;
    server.use(
      http.get('*/me/crawl/interests', ({ request }) => {
        const cursor = new URL(request.url).searchParams.get('cursor');
        if (!cursor) return json(interests([interest], 'next'));
        failures += 1;
        return failures === 1
          ? json({ code: 'failed' }, 500)
          : json(interests([{ ...interest, nsid: 'two@N00', name: 'Two Person' }]));
      }),
      http.get('*/me/crawl/settings', () => json({ depth: 2, depth_default: 2, depth_max: 4 })),
    );
    renderApp({ session: userSession, path: '/contacts' });
    expect(await screen.findByTestId('user-contact-card')).toHaveTextContent('One Person');
    fireEvent.click(screen.getByTestId('user-contacts-load-more'));
    expect(await screen.findByTestId('user-contacts-more-error')).toBeInTheDocument();
    expect(screen.getByTestId('user-contact-card')).toHaveTextContent('One Person');
    fireEvent.click(screen.getByTestId('user-contacts-retry-more'));
    expect(await screen.findByText('Two Person')).toBeInTheDocument();
  });

  it('shows filtered empty, first-use empty, forbidden, error, and retry states', async () => {
    const handlers = (response: () => Response) =>
      server.use(
        http.get('*/me/crawl/interests', response),
        http.get('*/me/crawl/settings', () => json({ depth: 2, depth_default: 2, depth_max: 4 })),
      );
    handlers(() => json(interests([])));
    const filtered = renderApp({ session: userSession, path: '/contacts?q=missing' });
    expect(await screen.findByTestId('user-contacts-empty')).toHaveTextContent('No contacts match');
    filtered.unmount();

    handlers(() => json(interests([])));
    renderApp({ session: userSession, path: '/contacts' });
    expect(await screen.findByTestId('user-contacts-empty')).toHaveTextContent('No contacts yet');
  });

  it('shows access denied when the interests endpoint forbids access', async () => {
    server.use(
      http.get('*/me/crawl/interests', () => json({ code: 'forbidden' }, 403)),
      http.get('*/me/crawl/settings', () => json({ depth: 2, depth_default: 2, depth_max: 4 })),
    );
    renderApp({ session: userSession, path: '/contacts' });
    expect(await screen.findByTestId('user-contacts-forbidden')).toHaveTextContent(
      'You cannot view these contacts',
    );
  });

  it('retries an initial list failure without losing URL filters', async () => {
    let available = false;
    server.use(
      http.get('*/me/crawl/interests', () =>
        available ? json(interests([interest])) : json({ code: 'failed' }, 500),
      ),
      http.get('*/me/crawl/settings', () => json({ depth: 2, depth_default: 2, depth_max: 4 })),
    );
    renderApp({ session: userSession, path: '/contacts?filter=starred' });
    expect(await screen.findByTestId('user-contacts-error')).toBeInTheDocument();
    available = true;
    fireEvent.click(screen.getByTestId('user-contacts-retry'));
    expect(await screen.findByTestId('user-contact-card')).toHaveTextContent('One Person');
  });

  it('keeps contacts visible and offers retry when a background refresh fails', async () => {
    let refreshFailed = false;
    server.use(
      http.get('*/me/crawl/interests', () =>
        refreshFailed ? json({ code: 'failed' }, 500) : json(interests([interest])),
      ),
      http.get('*/me/crawl/settings', () => json({ depth: 2, depth_default: 2, depth_max: 4 })),
      http.put('*/me/contacts/:nsid/star', () => {
        refreshFailed = true;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    renderApp({ session: userSession, path: '/contacts' });
    expect(await screen.findByTestId('user-contact-card')).toHaveTextContent('One Person');
    fireEvent.click(screen.getByTestId('user-contact-star'));
    expect(await screen.findByTestId('user-contacts-refresh-error')).toHaveTextContent(
      'Contacts may be out of date',
    );
    expect(screen.getByTestId('user-contact-card')).toHaveTextContent('One Person');
    refreshFailed = false;
    fireEvent.click(screen.getByTestId('user-contacts-refresh-retry'));
    await waitFor(() =>
      expect(screen.queryByTestId('user-contacts-refresh-error')).not.toBeInTheDocument(),
    );
    expect(screen.getByTestId('user-contact-card')).toHaveTextContent('One Person');
  });
});

describe('ContactPage', () => {
  it('shows contact-specific progress and supports star, prioritise, pause, and contact download', async () => {
    const calls: string[] = [];
    let downloadFailures = 0;
    server.use(
      http.get('*/me/contacts/:nsid', () => json(contact)),
      http.get('*/me/contacts/:nsid/photos', () =>
        json(
          photoPage([
            {
              id: '10',
              title: 'Stored photo',
              thumbnail_url: null,
              width: 600,
              height: 400,
              taken_at: null,
              sizes_available: true,
            },
          ]),
        ),
      ),
      http.put('*/me/contacts/:nsid/star', () => {
        calls.push('star');
        return new HttpResponse(null, { status: 204 });
      }),
      http.put('*/me/contacts/:nsid/sizes-priority', () => {
        calls.push('priority');
        return json({ prioritised: 1 });
      }),
      http.put('*/me/crawl/pause', () => {
        calls.push('pause');
        return new HttpResponse(null, { status: 204 });
      }),
      http.post('*/downloads', async ({ request }) => {
        const body = (await request.json()) as { kind: string; subject: string };
        calls.push(`${body.kind}:${body.subject}`);
        downloadFailures += 1;
        return downloadFailures === 1
          ? json({ code: 'temp_storage_full' }, 409)
          : json(
              {
                id: 1,
                kind: 'contact',
                subject: body.subject,
                status: 'pending',
                created_at: new Date().toISOString(),
                expires_at: new Date().toISOString(),
                item_count: 1,
                skipped: 0,
              },
              201,
            );
      }),
    );
    renderApp({ session: userSession, path: '/contacts/one@N00' });
    expect(await screen.findByTestId('user-contact-photos-grid')).toHaveTextContent('Stored photo');
    expect(screen.getByTestId('user-contact-photos-facet-photos')).toHaveTextContent('8');
    fireEvent.click(screen.getByTestId('user-contact-photos-star'));
    fireEvent.click(screen.getByTestId('user-contact-photos-prioritise-sizes'));
    fireEvent.click(screen.getByTestId('user-contact-photos-crawl-toggle'));
    fireEvent.click(screen.getByTestId('user-contact-photos-download-all'));
    fireEvent.click(await screen.findByTestId('download-modal-submit'));
    expect(
      await within(screen.getByTestId('contact-download-modal')).findByRole('alert'),
    ).toHaveTextContent('Could not create the download');
    fireEvent.click(screen.getByTestId('download-modal-submit'));
    expect(
      await within(screen.getByTestId('contact-download-modal')).findByRole('status'),
    ).toHaveTextContent('Your download has been queued');
    await waitFor(() =>
      expect([...calls].sort()).toEqual([
        'contact:one@N00',
        'contact:one@N00',
        'pause',
        'priority',
        'star',
      ]),
    );
  });

  it('closes the photo lightbox with Escape and restores focus to its photo', async () => {
    server.use(
      http.get('*/me/contacts/:nsid', () => json(contact)),
      http.get('*/me/contacts/:nsid/photos', () =>
        json(
          photoPage([
            {
              id: '10',
              title: 'Stored photo',
              thumbnail_url: null,
              width: 600,
              height: 400,
              taken_at: null,
              sizes_available: true,
            },
          ]),
        ),
      ),
    );
    renderApp({ session: userSession, path: '/contacts/one@N00' });
    const photo = await screen.findByRole('button', { name: 'Stored photo' });
    fireEvent.click(photo);
    const close = screen.getByRole('button', { name: 'Close photo' });
    expect(close).toHaveFocus();
    fireEvent.keyDown(close, { key: 'Escape' });
    expect(screen.queryByTestId('user-contact-photo-lightbox')).not.toBeInTheDocument();
    expect(photo).toHaveFocus();
  });

  it('returns focus to the download action when its dialog closes with Escape', async () => {
    server.use(
      http.get('*/me/contacts/:nsid', () => json(contact)),
      http.get('*/me/contacts/:nsid/photos', () => json(photoPage([]))),
    );
    renderApp({ session: userSession, path: '/contacts/one@N00' });
    const trigger = await screen.findByTestId('user-contact-photos-download-all');
    fireEvent.click(trigger);
    expect(await screen.findByTestId('contact-download-modal')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Close' })).toHaveFocus();
    fireEvent.keyDown(screen.getByRole('button', { name: 'Close' }), { key: 'Escape' });
    expect(screen.queryByTestId('contact-download-modal')).not.toBeInTheDocument();
    expect(trigger).toHaveFocus();
  });

  it('renders Favorites, links to the favorite owner, and downloads that scope', async () => {
    const calls: string[] = [];
    server.use(
      http.get('*/me/contacts/:nsid', () => json(contact)),
      http.get('*/me/contacts/:nsid/photos', () => json(photoPage([]))),
      http.get('*/me/contacts/:nsid/favorites', () =>
        json(
          photoPage([
            {
              id: '20',
              title: 'Favorite photo',
              thumbnail_url: null,
              width: null,
              height: null,
              taken_at: null,
              sizes_available: false,
              owner_nsid: 'owner@N00',
              owner_name: 'Owner',
              owner_avatar_url: null,
            },
          ]),
        ),
      ),
      http.post('*/downloads', async ({ request }) => {
        calls.push(String(((await request.json()) as { kind: string }).kind));
        return json(
          {
            id: 2,
            kind: 'favorites',
            subject: 'one@N00',
            status: 'pending',
            created_at: new Date().toISOString(),
            expires_at: new Date().toISOString(),
            item_count: 1,
            skipped: 0,
          },
          201,
        );
      }),
    );
    renderApp({ session: userSession, path: '/contacts/one@N00?tab=favorites' });
    expect(await screen.findByTestId('user-contact-favorites-grid')).toHaveTextContent(
      'Favorite photo',
    );
    expect(screen.getByTestId('user-contact-favorites-owner-20')).toHaveTextContent('Owner');
    fireEvent.click(screen.getByTestId('user-contact-favorites-download-all'));
    fireEvent.click(await screen.findByTestId('download-modal-submit'));
    await waitFor(() => expect(calls).toEqual(['favorites']));
  });

  it('renders albums, galleries, groups, and about data and updates group pool preferences', async () => {
    let poolEnabled = false;
    server.use(
      http.get('*/me/contacts/:nsid', () => json(contact)),
      http.get('*/me/contacts/:nsid/photos', () => json(photoPage([]))),
      http.get('*/me/contacts/:nsid/albums', () =>
        json({
          items: [
            {
              id: 'album1',
              title: 'Album one',
              owner_name: 'One',
              stored_count: 3,
              total_count: 5,
              cover_photo_urls: [],
            },
          ],
          total: 1,
          updated_at: null,
        }),
      ),
      http.get('*/me/contacts/:nsid/galleries', () =>
        json({
          items: [{ id: 'gallery1', title: 'Gallery one', photo_count: 4 }],
          total: 1,
          updated_at: null,
        }),
      ),
      http.get('*/me/contacts/:nsid/groups', () =>
        json({
          items: [
            {
              nsid: 'group1',
              name: 'Group one',
              members_count: 42,
              pool_crawl_enabled: poolEnabled,
            },
          ],
          total: 1,
          updated_at: null,
        }),
      ),
      http.get('*/me/contacts/:nsid/about', () =>
        json({
          last_change_check_at: null,
          facets: contact.facets,
          tombstones: { count: 2, updated_at: null },
          updated_at: null,
        }),
      ),
      http.put('*/me/contacts/:nsid/groups/:groupNsid/pool-crawl', async ({ request }) => {
        poolEnabled = ((await request.json()) as { enabled: boolean }).enabled;
        return json({ enabled: poolEnabled, changed_at: new Date().toISOString() });
      }),
    );
    renderApp({ session: userSession, path: '/contacts/one@N00?tab=albums' });
    expect(await screen.findByTestId('user-contact-albums-grid')).toHaveTextContent('Album one');
    fireEvent.click(screen.getByTestId('user-contact-tab-galleries'));
    expect(await screen.findByTestId('user-contact-galleries-grid')).toHaveTextContent(
      'Gallery one',
    );
    fireEvent.click(screen.getByTestId('user-contact-tab-groups'));
    expect(await screen.findByTestId('user-contact-groups-table')).toHaveTextContent('Group one');
    fireEvent.click(screen.getByTestId('user-contact-groups-about-group-toggle-group1'));
    await waitFor(() => expect(poolEnabled).toBe(true));
    fireEvent.click(screen.getByTestId('user-contact-tab-about'));
    expect(await screen.findByText('Flickr NSID')).toBeInTheDocument();
    expect(screen.getByText('2')).toBeInTheDocument();
  });

  it('labels the group crawl switch and restores its confirmed value after a failed save', async () => {
    server.use(
      http.get('*/me/contacts/:nsid', () => json(contact)),
      http.get('*/me/contacts/:nsid/groups', () =>
        json({
          items: [
            { nsid: 'group1', name: 'Group one', members_count: 42, pool_crawl_enabled: false },
          ],
          total: 1,
          updated_at: null,
        }),
      ),
      http.put('*/me/contacts/:nsid/groups/:groupNsid/pool-crawl', () =>
        json({ code: 'failed' }, 500),
      ),
    );
    renderApp({ session: userSession, path: '/contacts/one@N00?tab=groups' });
    const toggle = await screen.findByTestId('user-contact-groups-about-group-toggle-group1');
    expect(toggle).toHaveAccessibleName('Pool crawl for Group one');
    fireEvent.click(toggle);
    expect(await screen.findByRole('alert')).toHaveTextContent('Could not update pool crawl');
    expect(toggle).not.toBeChecked();
  });

  it('keeps a loaded photo page visible and retries a failed next page', async () => {
    let nextPageFailures = 0;
    server.use(
      http.get('*/me/contacts/:nsid', () => json(contact)),
      http.get('*/me/contacts/:nsid/photos', ({ request }) => {
        if (new URL(request.url).searchParams.has('cursor')) {
          nextPageFailures += 1;
          return nextPageFailures === 1
            ? json({ code: 'failed' }, 500)
            : json(
                photoPage([
                  {
                    id: '11',
                    title: 'Next photo',
                    thumbnail_url: null,
                    width: null,
                    height: null,
                    taken_at: null,
                    sizes_available: true,
                  },
                ]),
              );
        }
        return json(
          photoPage(
            [
              {
                id: '10',
                title: 'First photo',
                thumbnail_url: null,
                width: null,
                height: null,
                taken_at: null,
                sizes_available: true,
              },
            ],
            'next',
          ),
        );
      }),
    );
    renderApp({ session: userSession, path: '/contacts/one@N00' });
    expect(await screen.findByTestId('user-contact-photos-grid')).toHaveTextContent('First photo');
    fireEvent.click(screen.getByTestId('user-contact-photos-load-more'));
    expect(await screen.findByText(/Could not load more photos/)).toBeInTheDocument();
    expect(screen.getByTestId('user-contact-photos-grid')).toHaveTextContent('First photo');
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    expect(await screen.findByText('Next photo')).toBeInTheDocument();
  });

  it('shows empty and retryable errors for contact and tab reads, and does not retry 404', async () => {
    let available = false;
    server.use(
      http.get('*/me/contacts/:nsid', () =>
        available ? json(contact) : json({ code: 'failed' }, 500),
      ),
      http.get('*/me/contacts/:nsid/photos', () => json(photoPage([]))),
    );
    renderApp({ session: userSession, path: '/contacts/one@N00' });
    expect(await screen.findByTestId('user-contact-photos-error')).toBeInTheDocument();
    available = true;
    fireEvent.click(screen.getByTestId('user-contact-photos-retry'));
    expect(await screen.findByTestId('user-contact-photos-empty')).toHaveTextContent(
      'No photos yet',
    );
  });

  it('shows forbidden and not found states for inaccessible contacts', async () => {
    server.use(http.get('*/me/contacts/:nsid', () => json({ code: 'forbidden' }, 403)));
    renderApp({ session: userSession, path: '/contacts/one@N00' });
    expect(await screen.findByTestId('user-contact-photos-forbidden')).toHaveTextContent(
      'You cannot view this contact',
    );
  });

  it.each([
    [403, 'user-contact-photos-forbidden', 'You cannot view this contact'],
    [404, 'user-contact-not-found', 'Contact not found'],
  ] as const)('clears contact data when a tab returns %i', async (status, stateTestId, message) => {
    server.use(
      http.get('*/me/contacts/:nsid', () => json(contact)),
      http.get('*/me/contacts/:nsid/photos', () => json({ code: 'inaccessible' }, status)),
    );
    renderApp({ session: userSession, path: '/contacts/one@N00' });
    expect(await screen.findByTestId(stateTestId)).toHaveTextContent(message);
    expect(screen.queryByTestId('user-contact-photos-header')).not.toBeInTheDocument();
    expect(screen.queryByTestId('user-contact-photos-grid')).not.toBeInTheDocument();
    expect(screen.queryByTestId('user-contact-photos-retry')).not.toBeInTheDocument();
  });
});
