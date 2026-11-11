import { fireEvent, screen } from '@testing-library/react';
import { http, HttpResponse } from 'msw/http';
import { describe, expect, it } from 'vitest';
import { server } from '../../test/server';
import { renderApp, userSession } from '../../test/render';

const snapshot = {
  generated_at: '2026-11-12T00:00:00Z',
  contacts: { total: 1234, direct: 1000, discovered: 234 },
  photos: { total: null, sizes_ready: null, captioned: null },
  unavailable_sections: ['photos', 'starred_contacts', 'activity'],
  links: { self: { href: '/api/v1/me/home', method: 'GET' } },
};

const envelope = (data: unknown) => ({
  success: true,
  code: 200,
  message: 'OK',
  data,
  meta: {},
  errors: null,
  trace_id: 'test-trace',
});
const json = (body: unknown, status = 200) => HttpResponse.json(body as never, { status });
const homeEndpoint = '*/api/v1/me/home';
const open = (locale: 'en' | 'vi' = 'en') => renderApp({ session: userSession, locale });

describe('HomePage', () => {
  it.each([
    ['en', 'Home', '1,234', '1,000 direct', '234 discovered'],
    ['vi', 'Trang chủ', '1.234', '1.000 trực tiếp', '234 được khám phá'],
  ] as const)(
    'shows real contact counts in %s',
    async (locale, title, total, direct, discovered) => {
      server.use(http.get(homeEndpoint, () => json(envelope(snapshot))));
      const { container } = open(locale);

      expect(await screen.findByRole('heading', { level: 1, name: title })).toBeInTheDocument();
      expect(document.title).toBe(`${title} · XFlickr`);
      expect(await screen.findByTestId('user-home-stat-contacts')).toHaveTextContent(total);
      expect(screen.getByTestId('user-home-stat-direct')).toHaveTextContent(direct);
      expect(screen.getByTestId('user-home-stat-discovered')).toHaveTextContent(discovered);
      expect(container.querySelectorAll('h1')).toHaveLength(1);
      expect(screen.getByTestId('user-home-page').querySelectorAll('a')).toHaveLength(0);
      expect(screen.getByTestId('user-home-page')).not.toHaveTextContent(/photo|starred|activity/i);
    },
  );

  it('shows zero counts as zero', async () => {
    server.use(
      http.get(homeEndpoint, () =>
        json(
          envelope({
            ...snapshot,
            contacts: { total: 0, direct: 0, discovered: 0 },
          }),
        ),
      ),
    );
    open();

    for (const testId of [
      'user-home-stat-contacts',
      'user-home-stat-direct',
      'user-home-stat-discovered',
    ]) {
      expect(await screen.findByTestId(testId)).toHaveTextContent('0');
    }
  });

  it('keeps the shell visible while contact counts load', async () => {
    let resolveRequest!: () => void;
    const pending = new Promise<void>((resolve) => {
      resolveRequest = resolve;
    });
    server.use(
      http.get(homeEndpoint, async () => {
        await pending;
        return json(envelope(snapshot));
      }),
    );
    open();

    const skeleton = await screen.findByTestId('user-home-skeleton');
    expect(skeleton.parentElement).toHaveAttribute('aria-busy', 'true');
    expect(screen.getByTestId('shell-nav')).toBeInTheDocument();

    resolveRequest();
    expect(await screen.findByTestId('user-home-stat-contacts')).toHaveTextContent('1,234');
  });

  it('retries a failed request without showing the server error text', async () => {
    let requests = 0;
    server.use(
      http.get(homeEndpoint, () => {
        requests += 1;
        return requests === 1
          ? json({ message: 'PRIVATE_SERVER_DETAIL' }, 500)
          : json(envelope(snapshot));
      }),
    );
    open();

    expect(await screen.findByTestId('user-home-error')).toBeInTheDocument();
    const alert = screen.getByRole('alert');
    expect(alert).not.toHaveTextContent('PRIVATE_SERVER_DETAIL');
    fireEvent.click(screen.getByTestId('user-home-error-retry'));

    expect(await screen.findByTestId('user-home-stat-contacts')).toHaveTextContent('1,234');
    expect(requests).toBe(2);
  });

  it('treats a successful response without data as a retryable error', async () => {
    server.use(
      http.get(homeEndpoint, () =>
        json({
          success: true,
          code: 200,
          message: 'PRIVATE_DETAIL',
          meta: {},
          errors: null,
          trace_id: 'test-trace',
        }),
      ),
    );
    open();

    const alert = await screen.findByTestId('user-home-error');
    expect(alert).toHaveTextContent('Home could not load');
    expect(alert).not.toHaveTextContent('PRIVATE_DETAIL');
    expect(screen.getByTestId('user-home-error-retry')).toBeEnabled();
  });

  it('shows the forbidden state with only a sign-in link on 403', async () => {
    server.use(http.get(homeEndpoint, () => json({ message: 'PRIVATE_DETAIL' }, 403)));
    open();

    expect(await screen.findByTestId('user-home-forbidden')).toHaveTextContent(
      'You cannot view Home',
    );
    expect(screen.getByTestId('user-home-forbidden').querySelector('a')).toHaveAttribute(
      'href',
      '/login',
    );
    expect(screen.queryByTestId('user-home-error-retry')).not.toBeInTheDocument();
    expect(screen.getByTestId('user-home-forbidden')).not.toHaveTextContent('PRIVATE_DETAIL');
  });

  it('lets the shared shell handle a 401 response', async () => {
    server.use(http.get(homeEndpoint, () => json({ message: 'session ended' }, 401)));
    open();

    expect(await screen.findByTestId('shell-session-modal')).toBeInTheDocument();
    expect(screen.queryByTestId('user-home-error')).not.toBeInTheDocument();
  });
});
