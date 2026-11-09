import { fireEvent, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw/http';
import { beforeEach, describe, expect, it } from 'vitest';
import { server } from '../../test/server';
import { renderApp, userSession } from '../../test/render';
import { statusFixture } from '../../test/crawl';

const people = [
  { nsid: 'one@N00', username: 'one', realname: 'One Person', icon: '', photos_count: 12 },
  { nsid: 'two@N00', username: 'two', realname: 'Two Person', icon: '', photos_count: 7 },
];

const json = (body: unknown, status = 200) => HttpResponse.json(body as never, { status });

beforeEach(() => {
  server.resetHandlers();
});

describe('ContactsPage', () => {
  it('lists real people, follows the keyset cursor, and opens the contact page', async () => {
    server.use(
      http.get('*/people', ({ request }) => {
        const cursor = new URL(request.url).searchParams.get('cursor');
        return cursor === null
          ? json({ items: [people[0]], next_cursor: people[0].nsid })
          : json({ items: [people[1]] });
      }),
      http.get('*/me/crawl/settings', () => json({ depth: 3, depth_default: 2, depth_max: 4 })),
      http.get('*/people/:nsid', ({ params }) =>
        json(people.find((person) => person.nsid === params.nsid) ?? {}, 200),
      ),
      http.get('*/me/crawl', () => json(statusFixture)),
    );

    const { router } = renderApp({ session: userSession, path: '/contacts' });

    expect(await screen.findByTestId('user-contacts-card')).toHaveTextContent('One Person');
    expect(screen.getByTestId('user-contacts-title')).toHaveTextContent('Contacts');
    expect(screen.getByTestId('user-contacts-depth')).toHaveTextContent('3');
    fireEvent.click(screen.getByTestId('user-contacts-load-more'));
    expect(await screen.findByText('Two Person')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('link', { name: 'View One Person' }));

    await waitFor(() => expect(router.state.location.pathname).toBe('/contacts/one%40N00'));
    expect(await screen.findByTestId('user-contact-photo-count')).toHaveTextContent('12');
    expect(await screen.findByTestId('user-contact-crawl')).toHaveTextContent(
      'Your crawl progress',
    );
    expect(screen.getByTestId('user-contact-crawl')).toHaveTextContent('130 of 341 complete');
    expect(screen.getByTestId('user-contact-tab-photos')).toHaveAttribute('aria-current', 'page');
    expect(screen.getByTestId('user-contact-tab-albums')).toHaveAttribute(
      'href',
      '/contacts/one%40N00?tab=albums',
    );
  });

  it('shows the first-use empty state and its setup action', async () => {
    server.use(
      http.get('*/people', () => json({ items: [] })),
      http.get('*/me/crawl/settings', () => json({ depth: 2, depth_default: 2, depth_max: 4 })),
    );

    const { router } = renderApp({ session: userSession, path: '/contacts' });

    expect(await screen.findByTestId('user-contacts-empty')).toHaveTextContent('No contacts yet');
    fireEvent.click(screen.getByTestId('user-contacts-empty-setup'));
    await waitFor(() => expect(router.state.location.pathname).toBe('/onboarding'));
  });

  it('shows the access-denied state when contacts are forbidden', async () => {
    server.use(
      http.get('*/people', () => json({ code: 'forbidden' }, 403)),
      http.get('*/me/crawl/settings', () => json({ depth: 2, depth_default: 2, depth_max: 4 })),
    );

    renderApp({ session: userSession, path: '/contacts' });

    expect(await screen.findByTestId('user-contacts-forbidden')).toHaveTextContent(
      'You cannot view these contacts',
    );
  });

  it('shows a retryable error when the list request fails', async () => {
    let available = false;
    server.use(
      http.get('*/people', () =>
        available ? json({ items: [people[0]] }) : json({ code: 'failed' }, 500),
      ),
      http.get('*/me/crawl/settings', () => json({ depth: 2, depth_default: 2, depth_max: 4 })),
    );

    renderApp({ session: userSession, path: '/contacts' });
    expect(await screen.findByTestId('user-contacts-error')).toBeInTheDocument();
    available = true;
    fireEvent.click(screen.getByTestId('user-contacts-retry'));
    expect(await screen.findByTestId('user-contacts-card')).toHaveTextContent('One Person');
  });

  it('shows not found when a contact is not visible', async () => {
    server.use(http.get('*/people/:nsid', () => json({ code: 'not_found' }, 404)));

    renderApp({ session: userSession, path: '/contacts/missing@N00' });

    expect(await screen.findByTestId('user-contact-not-found')).toHaveTextContent(
      'Contact not found',
    );
  });

  it('shows the access-denied state for a forbidden contact response', async () => {
    server.use(http.get('*/people/:nsid', () => json({ code: 'forbidden' }, 403)));

    renderApp({ session: userSession, path: '/contacts/one@N00' });

    expect(await screen.findByTestId('user-contact-forbidden')).toHaveTextContent(
      'You cannot view this contact',
    );
  });

  it('shows a retryable contact error and recovers after retry', async () => {
    let available = false;
    server.use(
      http.get('*/people/:nsid', () =>
        available ? json(people[0]) : json({ code: 'failed' }, 500),
      ),
    );

    renderApp({ session: userSession, path: '/contacts/one@N00' });

    expect(await screen.findByTestId('user-contact-error')).toBeInTheDocument();
    available = true;
    fireEvent.click(screen.getByTestId('user-contact-retry'));
    await waitFor(() =>
      expect(screen.getByTestId('user-contact-title')).toHaveTextContent('One Person'),
    );
  });

  it('keeps the contact header when the aggregate crawl progress is unavailable', async () => {
    server.use(
      http.get('*/people/:nsid', () => json(people[0])),
      http.get('*/me/crawl', () => json({ code: 'failed' }, 500)),
    );

    renderApp({ session: userSession, path: '/contacts/one@N00', locale: 'vi' });

    expect(await screen.findByTestId('user-contact-title')).toHaveTextContent('One Person');
    expect(await screen.findByTestId('user-contact-crawl')).toHaveTextContent(
      'Không thể tải tiến độ thu thập.',
    );
    expect(screen.getByTestId('user-contact-photo-count')).toHaveTextContent('12');
  });

  it('explains when no crawl progress has been recorded', async () => {
    server.use(
      http.get('*/people/:nsid', () => json(people[0])),
      http.get('*/me/crawl', () => json({ depths: [], facets: [], lanes: [] })),
    );

    renderApp({ session: userSession, path: '/contacts/one@N00' });

    expect(await screen.findByTestId('user-contact-crawl')).toHaveTextContent(
      'No crawl progress is available yet.',
    );
  });
});
