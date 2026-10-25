import { fireEvent, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw/http';
import { describe, expect, it } from 'vitest';
import { server } from '../../test/server';
import { renderApp, userSession } from '../../test/render';
import { queueFixture, settingsFixture, statusFixture } from '../../test/crawl';

const json = (body: unknown, status = 200) => HttpResponse.json(body as never, { status });
const fail = (status = 500, body: unknown = {}) => HttpResponse.json(body as never, { status });

const open = (path = '/crawl', locale: 'en' | 'vi' = 'en') =>
  renderApp({ session: userSession, path, locale });

const queueWith = (patch: Record<string, unknown>) =>
  server.use(http.get('*/me/crawl/queue', () => json({ ...queueFixture, ...patch })));

describe('CrawlPage bad responses', () => {
  it.each([
    [
      'HTML with status 200',
      () =>
        new HttpResponse('<!doctype html><html>app</html>', {
          status: 200,
          headers: { 'Content-Type': 'text/html' },
        }),
    ],
    ['a null body', () => HttpResponse.json(null)],
  ])('shows the retryable error when every endpoint answers %s', async (_name, reply) => {
    server.use(http.get('*/me/crawl/*', () => reply()));
    open();
    expect(await screen.findByTestId('user-crawl-error')).toBeInTheDocument();
    expect(screen.getByTestId('user-crawl-error-retry')).toBeEnabled();
  });
});

describe('CrawlPage ready state', () => {
  it('shows depth, lanes with ETA, per-depth columns and the not-available block', async () => {
    open();

    expect(await screen.findByTestId('user-crawl-title')).toHaveTextContent('Crawl');
    expect(document.title).toBe('Crawl · XFlickr');
    expect(await screen.findByTestId('user-crawl-lane-eta-person')).toHaveTextContent(
      'ETA ~3 h 20 min',
    );
    expect(screen.getByTestId('user-crawl-lane-backlog-person')).toHaveTextContent(
      '1,200 calls waiting',
    );
    expect(screen.getByTestId('user-crawl-lane-eta-sizes')).toHaveTextContent('–');
    expect(screen.getByTestId('user-crawl-depth-value')).toHaveTextContent('Depth 2');
    expect(screen.getByTestId('user-crawl-depth-cap')).toHaveTextContent(
      'Default 2, your maximum 4.',
    );
    expect(await screen.findByTestId('user-crawl-depth-column-0')).toHaveTextContent(
      'Your accounts',
    );
    expect(screen.getByTestId('user-crawl-depth-column-3')).toHaveTextContent('Above setting');
    expect(screen.getByTestId('user-crawl-unavailable')).toHaveTextContent('Not available yet');
    expect(screen.queryByTestId('user-crawl-stale-banner')).not.toBeInTheDocument();
  });

  it('renders Vietnamese copy', async () => {
    open('/crawl', 'vi');

    expect(await screen.findByRole('heading', { level: 1, name: 'Thu thập' })).toBeInTheDocument();
    expect(await screen.findByTestId('user-crawl-lane-eta-person')).toHaveTextContent(
      'Dự kiến ~3 giờ 20 phút',
    );
  });

  it.each([
    [0.005, '< 1 min'],
    [0.5, '~30 min'],
    [2, '~2 h'],
  ])('formats an ETA of %s hours as %s', async (hours, text) => {
    queueWith({ lanes: [{ ...queueFixture.lanes[0], eta_hours: hours }, queueFixture.lanes[1]] });
    open();

    expect(await screen.findByTestId('user-crawl-lane-eta-person')).toHaveTextContent(text);
  });

  it('shows the depth warning only when far above the default', async () => {
    server.use(http.get('*/me/crawl/settings', () => json({ ...settingsFixture, depth_max: 6 })));
    open();
    const slider = await screen.findByTestId('user-crawl-depth-slider');
    expect(
      screen.queryByText('Each level can multiply contacts by 10 or more.'),
    ).not.toBeInTheDocument();
    fireEvent.change(slider, { target: { value: '5' } });
    expect(screen.getByText('Each level can multiply contacts by 10 or more.')).toBeInTheDocument();
  });
});

describe('CrawlPage depth', () => {
  it('saves a changed depth and toasts', async () => {
    let body: unknown;
    server.use(
      http.put('*/me/crawl/settings', async ({ request }) => {
        body = await request.json();
        return json({ ...settingsFixture, depth: 3 });
      }),
    );
    open();
    const slider = await screen.findByTestId('user-crawl-depth-slider');
    expect(screen.queryByTestId('user-crawl-depth-save')).not.toBeInTheDocument();
    fireEvent.change(slider, { target: { value: '3' } });
    fireEvent.click(screen.getByTestId('user-crawl-depth-save'));

    await waitFor(() => expect(body).toEqual({ depth: 3 }));
    expect(await screen.findByText('Depth set to 3')).toBeInTheDocument();
    await waitFor(() =>
      expect(screen.queryByTestId('user-crawl-depth-save')).not.toBeInTheDocument(),
    );
  });

  it('resets the slider and explains the cap on 422', async () => {
    server.use(
      http.put('*/me/crawl/settings', () =>
        fail(422, { code: 'depth_above_cap', details: { max_depth: 3 } }),
      ),
    );
    open();
    fireEvent.change(await screen.findByTestId('user-crawl-depth-slider'), {
      target: { value: '4' },
    });
    fireEvent.click(screen.getByTestId('user-crawl-depth-save'));

    expect(
      await screen.findByText('Your maximum depth is 3. Pick a lower value.'),
    ).toBeInTheDocument();
    expect(screen.getByTestId('user-crawl-depth-value')).toHaveTextContent('Depth 2');
  });

  it('reports a failed save as a bad toast', async () => {
    server.use(http.put('*/me/crawl/settings', () => fail(500)));
    open();
    fireEvent.change(await screen.findByTestId('user-crawl-depth-slider'), {
      target: { value: '3' },
    });
    fireEvent.click(screen.getByTestId('user-crawl-depth-save'));

    expect(await screen.findByText('That did not work. Nothing changed.')).toBeInTheDocument();
    expect(screen.getByTestId('user-crawl-depth-value')).toHaveTextContent('Depth 2');
  });
});

describe('CrawlPage states', () => {
  const paths = ['/me/crawl/queue', '/me/crawl/settings', '/me/crawl'];

  it('shows the error state when every request fails and retries them', async () => {
    let healthy = false;
    server.use(
      ...paths.map((path) => http.get(`*${path}`, () => (healthy ? undefined : fail(500)))),
    );
    open();

    expect(await screen.findByTestId('user-crawl-error')).toHaveTextContent('Crawl could not load');
    healthy = true;
    server.resetHandlers();
    fireEvent.click(screen.getByTestId('user-crawl-error-retry'));

    expect(await screen.findByTestId('user-crawl-depths')).toBeInTheDocument();
    expect(screen.queryByTestId('user-crawl-error')).not.toBeInTheDocument();
  });

  it('shows the forbidden state on a first 403 and goes Home', async () => {
    server.use(
      ...paths.map((path) => http.get(`*${path}`, () => fail(403, { code: 'forbidden' }))),
    );
    const { router } = open();

    expect(await screen.findByTestId('user-crawl-forbidden')).toHaveTextContent(
      'You cannot view the crawl',
    );
    expect(screen.queryByTestId('user-crawl-error-retry')).not.toBeInTheDocument();
    fireEvent.click(screen.getByTestId('user-crawl-forbidden-cta'));
    await waitFor(() => expect(router.state.location.pathname).toBe('/'));
  });

  it('keeps the page and retries a single failed section', async () => {
    let healthy = false;
    server.use(
      http.get('*/me/crawl', () => (healthy ? json(statusFixture) : fail(500))),
      http.get('*/me/crawl/settings', () => fail(500)),
    );
    open();

    const section = await screen.findByTestId('user-crawl-section-error-depths');
    expect(section).toHaveTextContent('This section could not load.');
    expect(screen.getByTestId('user-crawl-section-error-settings')).toBeInTheDocument();
    healthy = true;
    fireEvent.click(screen.getByTestId('user-crawl-section-retry-depths'));

    expect(await screen.findByTestId('user-crawl-depths')).toBeInTheDocument();
  });

  it('shows the stale banner with dashes when the queue fails and retries it', async () => {
    let healthy = false;
    server.use(http.get('*/me/crawl/queue', () => (healthy ? json(queueFixture) : fail(500))));
    open();

    expect(await screen.findByTestId('user-crawl-stale-banner')).toHaveTextContent(
      'Queue numbers are out of date',
    );
    expect(screen.getByTestId('user-crawl-lane-eta-person')).toHaveTextContent('–');
    healthy = true;
    fireEvent.click(screen.getByTestId('user-crawl-stale-retry'));

    await waitFor(() =>
      expect(screen.queryByTestId('user-crawl-stale-banner')).not.toBeInTheDocument(),
    );
    expect(screen.getByTestId('user-crawl-lane-eta-person')).toHaveTextContent('~3 h 20 min');
  });

  it('treats a 200 answer that is not JSON (the SPA index) as a failure, not as data', async () => {
    server.use(
      http.get('*/me/crawl/queue', () => new HttpResponse('<html></html>', { status: 200 })),
    );
    open();

    expect(await screen.findByTestId('user-crawl-stale-banner')).toBeInTheDocument();
    expect(await screen.findByTestId('user-crawl-depths')).toBeInTheDocument();
  });

  it('shows the skeleton after the delay while the first answers are pending', async () => {
    let release: () => void = () => undefined;
    const gate = new Promise<void>((resolve) => {
      release = resolve;
    });
    server.use(
      ...paths.map((path) =>
        http.get(`*${path}`, async () => {
          await gate;
          return undefined;
        }),
      ),
    );
    open();

    expect(await screen.findByTestId('user-crawl-skeleton')).toBeInTheDocument();
    expect(screen.getByText('Loading crawl…')).toHaveAttribute('role', 'status');
    release();
    expect(await screen.findByTestId('user-crawl-depths')).toBeInTheDocument();
  });
});
