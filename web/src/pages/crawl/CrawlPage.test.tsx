import { act, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw/http';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { server } from '../../test/server';
import { renderApp, userSession } from '../../test/render';
import {
  depthsFixture,
  interest,
  jobFixture,
  queueFixture,
  settingsFixture,
} from '../../test/crawl';

const json = (body: unknown, status = 200) => HttpResponse.json(body as never, { status });
const fail = (status = 500, body: unknown = {}) => HttpResponse.json(body as never, { status });

class FakeSource {
  static last: FakeSource | undefined;
  listeners = new Map<string, ((event: { data: string }) => void)[]>();
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  closed = false;
  constructor() {
    FakeSource.last = this;
  }
  addEventListener(type: string, listener: (event: { data: string }) => void) {
    this.listeners.set(type, [...(this.listeners.get(type) ?? []), listener]);
  }
  close() {
    this.closed = true;
  }
  open() {
    act(() => {
      this.onopen?.();
    });
  }
  emit(type: string, data: unknown) {
    act(() => {
      for (const listener of this.listeners.get(type) ?? [])
        listener({ data: JSON.stringify(data) });
    });
  }
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
  FakeSource.last = undefined;
});

const open = (path = '/crawl', locale: 'en' | 'vi' = 'en') =>
  renderApp({ session: userSession, path, locale });

const queueWith = (patch: Record<string, unknown>) =>
  server.use(http.get('*/me/crawl/queue', () => json({ ...queueFixture, ...patch })));

describe('CrawlPage ready state', () => {
  it('shows depth, lanes with ETA and position, per-depth columns, contacts and jobs', async () => {
    open();

    expect(await screen.findByTestId('user-crawl-title')).toHaveTextContent('Crawl');
    expect(document.title).toBe('Crawl · XFlickr');
    expect(await screen.findByTestId('user-crawl-lane-eta-person')).toHaveTextContent(
      'ETA ~3 h 20 min',
    );
    expect(screen.getByTestId('user-crawl-lane-position-person')).toHaveTextContent(
      'Queue position #4',
    );
    expect(screen.getByTestId('user-crawl-lane-backlog-person')).toHaveTextContent(
      '1,200 calls waiting',
    );
    expect(screen.getByTestId('user-crawl-lane-eta-sizes')).toHaveTextContent('–');
    expect(screen.getByTestId('user-crawl-lane-position-sizes')).toHaveTextContent('–');
    expect(screen.getByTestId('user-crawl-depth-value')).toHaveTextContent('Depth 2');
    expect(screen.getByTestId('user-crawl-depth-cap')).toHaveTextContent(
      'Default 2, your maximum 4.',
    );
    expect(screen.getByTestId('user-crawl-recrawl-card')).toHaveTextContent(
      '143 of 353 contacts done',
    );
    expect(await screen.findByTestId('user-crawl-depth-column-0')).toHaveTextContent(
      'Your accounts',
    );
    expect(screen.getByTestId('user-crawl-depth-column-3')).toHaveTextContent('Above setting');
    expect(await screen.findByTestId('user-crawl-contact-p1')).toHaveTextContent('Contact p1');
    expect(screen.getByTestId('user-crawl-contact-status-p1')).toHaveTextContent('Crawling');
    expect(await screen.findByTestId('user-crawl-job-j1')).toHaveTextContent('Retrying');
    expect(screen.queryByTestId('user-crawl-banner-waiting')).not.toBeInTheDocument();
    expect(screen.getByTestId('user-crawl-pause-all')).toHaveTextContent('Pause crawl');
  });

  it('renders Vietnamese copy', async () => {
    open('/crawl', 'vi');

    expect(await screen.findByRole('heading', { level: 1, name: 'Thu thập' })).toBeInTheDocument();
    expect(await screen.findByTestId('user-crawl-lane-position-person')).toHaveTextContent(
      'Vị trí hàng đợi #4',
    );
    expect(screen.getByTestId('user-crawl-lane-eta-person')).toHaveTextContent(
      'Dự kiến ~3 giờ 20 phút',
    );
  });

  it.each([
    [0.005, '< 1 min'],
    [0.5, '~30 min'],
    [2, '~2 h'],
  ])('formats an ETA of %s hours as %s', async (hours, text) => {
    queueWith({
      lanes: [{ ...queueFixture.lanes[0], eta_hours: hours }, queueFixture.lanes[1]],
    });
    open();

    expect(await screen.findByTestId('user-crawl-lane-eta-person')).toHaveTextContent(text);
  });

  it('shows the depth warning only when far above the default', async () => {
    server.use(http.get('*/me/crawl/settings', () => json({ ...settingsFixture, max_depth: 6 })));
    open();
    const slider = await screen.findByTestId('user-crawl-depth-slider');
    expect(
      screen.queryByText('Each level can multiply contacts by 10 or more.'),
    ).not.toBeInTheDocument();
    fireEvent.change(slider, { target: { value: '5' } });
    expect(screen.getByText('Each level can multiply contacts by 10 or more.')).toBeInTheDocument();
  });
});

describe('CrawlPage depth and pause', () => {
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

  it('pauses and resumes the crawl from the head button', async () => {
    const bodies: unknown[] = [];
    let paused = false;
    server.use(
      http.put('*/me/crawl/settings', async ({ request }) => {
        const body = (await request.json()) as { paused: boolean };
        bodies.push(body);
        paused = body.paused;
        return json({ ...settingsFixture, paused });
      }),
    );
    open();
    fireEvent.click(await screen.findByTestId('user-crawl-pause-all'));

    expect(await screen.findByText('Crawl paused')).toBeInTheDocument();
    await waitFor(() =>
      expect(screen.getByTestId('user-crawl-pause-all')).toHaveTextContent('Resume crawl'),
    );
    fireEvent.click(screen.getByTestId('user-crawl-pause-all'));

    expect(await screen.findByText('Crawl resumed')).toBeInTheDocument();
    expect(bodies).toEqual([{ paused: true }, { paused: false }]);
  });

  it('reports a failed pause', async () => {
    server.use(http.put('*/me/crawl/settings', () => fail(500)));
    open();
    fireEvent.click(await screen.findByTestId('user-crawl-pause-all'));

    expect(await screen.findByText('That did not work. Nothing changed.')).toBeInTheDocument();
  });

  it('refreshes the CSRF token once and repeats the call', async () => {
    let puts = 0;
    let refreshed = 0;
    server.use(
      http.get('*/me', () => {
        refreshed += 1;
        return json({});
      }),
      http.put('*/me/crawl/settings', () => {
        puts += 1;
        return puts === 1
          ? fail(403, { code: 'csrf_rejected' })
          : json({ ...settingsFixture, paused: true });
      }),
    );
    open();
    fireEvent.click(await screen.findByTestId('user-crawl-pause-all'));

    expect(await screen.findByText('Crawl paused')).toBeInTheDocument();
    expect(puts).toBe(2);
    expect(refreshed).toBe(1);
  });

  it('shows the action error when the second CSRF attempt is also rejected', async () => {
    server.use(
      http.get('*/me', () => json({})),
      http.put('*/me/crawl/settings', () => fail(403, { code: 'csrf_rejected' })),
    );
    open();
    fireEvent.click(await screen.findByTestId('user-crawl-pause-all'));

    expect(await screen.findByText('That did not work. Nothing changed.')).toBeInTheDocument();
  });
});

describe('CrawlPage quota banners', () => {
  it('counts down to the reset and refetches at zero', async () => {
    let calls = 0;
    const resetsAt = new Date(Date.now() + 2000).toISOString();
    server.use(
      http.get('*/me/crawl/queue', () => {
        calls += 1;
        return json({
          ...queueFixture,
          state: 'waiting_quota',
          server_time: new Date().toISOString(),
          resets_at: resetsAt,
        });
      }),
    );
    open();

    const banner = await screen.findByTestId('user-crawl-banner-waiting');
    expect(banner).toHaveAttribute('role', 'status');
    expect(within(banner).getByTestId('user-crawl-banner-countdown').textContent).toMatch(
      /^00:0[12]$/,
    );
    expect(screen.getByTestId('user-crawl-banner-action')).toHaveAttribute('href', '/keys');
    expect(screen.getByTestId('user-crawl-lane-eta-person')).toHaveTextContent('Waiting for quota');
    await waitFor(() => expect(calls).toBeGreaterThanOrEqual(2), { timeout: 5000 });
    expect(screen.getByTestId('user-crawl-banner-countdown')).toHaveTextContent('00:00');
    await waitFor(() => expect(calls).toBeGreaterThanOrEqual(3), { timeout: 6000 });
  }, 15_000);

  it('names the exhausted keys', async () => {
    queueWith({
      state: 'key_exhausted',
      exhausted_keys: [
        { id: 1, label: 'Key A' },
        { id: 2, label: 'Key B' },
      ],
    });
    open();

    expect(await screen.findByTestId('user-crawl-banner-exhausted')).toHaveTextContent(
      'Key A, Key B reached its hourly cap.',
    );
    expect(screen.getByTestId('user-crawl-banner-action')).toHaveAttribute('href', '/keys');
  });

  it('asks for a key when there is none and blanks the lanes', async () => {
    queueWith({ state: 'no_key' });
    open();

    const banner = await screen.findByTestId('user-crawl-banner-no-key');
    expect(banner).toHaveAttribute('role', 'alert');
    expect(screen.getByTestId('user-crawl-banner-action')).toHaveAttribute('href', '/keys?add=1');
    expect(screen.getByTestId('user-crawl-lane-eta-person')).toHaveTextContent('–');
    expect(screen.getByTestId('user-crawl-lane-backlog-person')).toHaveTextContent('–');
  });

  it('offers Resume in the paused banner', async () => {
    queueWith({ state: 'paused' });
    server.use(
      http.get('*/me/crawl/settings', () => json({ ...settingsFixture, paused: true })),
      http.put('*/me/crawl/settings', () => json(settingsFixture)),
    );
    open();

    await screen.findByTestId('user-crawl-banner-paused');
    expect(screen.getByTestId('user-crawl-pause-all')).toHaveTextContent('Resume crawl');
    expect(screen.getByTestId('user-crawl-lane-eta-person')).toHaveTextContent('–');
    fireEvent.click(screen.getByTestId('user-crawl-banner-action'));

    expect(await screen.findByText('Crawl resumed')).toBeInTheDocument();
  });

  it('announces a change of run state once', async () => {
    vi.stubGlobal('EventSource', FakeSource);
    open();
    await screen.findByTestId('user-crawl-lane-eta-person');
    FakeSource.last?.open();
    FakeSource.last?.emit('crawl.queue', { ...queueFixture, state: 'paused' });

    expect(await screen.findByTestId('user-crawl-banner-paused')).toBeInTheDocument();
    expect(screen.getByTestId('user-crawl-live-region')).toHaveTextContent('Crawl is now paused.');
  });
});

describe('CrawlPage contacts', () => {
  it('stars optimistically and keeps the server answer', async () => {
    let body: unknown;
    server.use(
      http.put('*/me/crawl/interests/:id', async ({ request, params }) => {
        body = await request.json();
        return json(interest(String(params.id), { starred: true }));
      }),
    );
    open();
    const star = await screen.findByTestId('user-crawl-contact-star-p2');
    expect(star).toHaveAttribute('aria-label', 'Star Contact p2');
    fireEvent.click(star);

    await waitFor(() => expect(star).toHaveAttribute('aria-pressed', 'true'));
    expect(star).toHaveAttribute('aria-label', 'Remove star from Contact p2');
    expect(body).toEqual({ starred: true });
  });

  it('rolls a failed star back with a toast', async () => {
    server.use(http.put('*/me/crawl/interests/:id', () => fail(500)));
    open();
    const star = await screen.findByTestId('user-crawl-contact-star-p2');
    fireEvent.click(star);

    expect(await screen.findByText('That did not work. Nothing changed.')).toBeInTheDocument();
    expect(star).toHaveAttribute('aria-pressed', 'false');
  });

  it('ignores a second click while the request is in flight', async () => {
    let puts = 0;
    let release: () => void = () => undefined;
    const gate = new Promise<void>((resolve) => {
      release = resolve;
    });
    server.use(
      http.put('*/me/crawl/interests/:id', async ({ params }) => {
        puts += 1;
        await gate;
        return json(interest(String(params.id), { starred: true }));
      }),
    );
    open();
    const star = await screen.findByTestId('user-crawl-contact-star-p2');
    fireEvent.click(star);
    await waitFor(() => expect(star).toBeDisabled());
    fireEvent.click(star);
    release();

    await waitFor(() => expect(star).not.toBeDisabled());
    expect(puts).toBe(1);
  });

  it('prioritizes a contact and marks it selected', async () => {
    let body: unknown;
    server.use(
      http.put('*/me/crawl/interests/:id', async ({ request, params }) => {
        body = await request.json();
        return json(
          interest(String(params.id), {
            selected_until: new Date(Date.now() + 86_400_000).toISOString(),
          }),
        );
      }),
    );
    open();
    fireEvent.click(await screen.findByTestId('user-crawl-contact-prioritize-p2'));

    expect(await screen.findByText('Contact p2 moved to the front')).toBeInTheDocument();
    expect(body).toEqual({ selected: true });
    expect(
      within(screen.getByTestId('user-crawl-contact-p2')).getByText('Selected'),
    ).toBeInTheDocument();
  });

  it('pauses and resumes a contact', async () => {
    const bodies: unknown[] = [];
    server.use(
      http.put('*/me/crawl/interests/:id', async ({ request, params }) => {
        const sent = (await request.json()) as { paused: boolean };
        bodies.push(sent);
        return json(
          interest(String(params.id), {
            paused: sent.paused,
            status: sent.paused ? 'paused' : 'crawling',
          }),
        );
      }),
    );
    open();
    const pause = await screen.findByTestId('user-crawl-contact-pause-p2');
    expect(pause).toHaveTextContent('Pause');
    fireEvent.click(pause);

    await waitFor(() => expect(pause).toHaveTextContent('Resume'));
    expect(screen.getByTestId('user-crawl-contact-status-p2')).toHaveTextContent('Paused');
    fireEvent.click(pause);
    await waitFor(() => expect(pause).toHaveTextContent('Pause'));
    expect(bodies).toEqual([{ paused: true }, { paused: false }]);
  });

  it('removes a row when the contact is gone', async () => {
    server.use(http.put('*/me/crawl/interests/:id', () => fail(404)));
    open();
    fireEvent.click(await screen.findByTestId('user-crawl-contact-pause-p2'));

    await waitFor(() =>
      expect(screen.queryByTestId('user-crawl-contact-p2')).not.toBeInTheDocument(),
    );
  });

  it('filters by chip and mirrors the filter in the URL', async () => {
    const seen: string[] = [];
    server.use(
      http.get('*/me/crawl/interests', ({ request }) => {
        const url = new URL(request.url);
        seen.push(`${url.searchParams.get('filter')}|${url.searchParams.get('depth') ?? ''}`);
        return json({ items: [interest('p1', { starred: true })], next_cursor: null });
      }),
    );
    const { router } = open();
    fireEvent.click(await screen.findByTestId('user-crawl-filter-starred'));

    await waitFor(() => expect(seen).toContain('starred|'));
    expect(router.state.location.search).toBe('?filter=starred');
    expect(screen.getByTestId('user-crawl-filter-starred')).toHaveAttribute('aria-pressed', 'true');
    expect(document.activeElement?.tagName).toBe('CAPTION');
    fireEvent.click(screen.getByTestId('user-crawl-filter-all'));
    await waitFor(() => expect(router.state.location.search).toBe(''));
  });

  it('filters by depth column and clears an empty filter', async () => {
    const seen: string[] = [];
    server.use(
      http.get('*/me/crawl/interests', ({ request }) => {
        const url = new URL(request.url);
        seen.push(url.searchParams.get('depth') ?? '');
        return json({
          items: url.searchParams.get('depth') === '3' ? [] : [interest('p1')],
          next_cursor: null,
        });
      }),
    );
    open();
    const column = await screen.findByTestId('user-crawl-depth-column-3');
    fireEvent.click(column);

    expect(await screen.findByText('No contacts match this filter.')).toBeInTheDocument();
    expect(seen).toContain('3');
    expect(column).toHaveAttribute('aria-pressed', 'true');
    fireEvent.click(screen.getByTestId('user-crawl-filter-clear'));

    expect(await screen.findByTestId('user-crawl-contact-p1')).toBeInTheDocument();
    expect(screen.getByTestId('user-crawl-depth-column-3')).toHaveAttribute(
      'aria-pressed',
      'false',
    );
  });

  it('loads more contacts with the cursor', async () => {
    const cursors: (string | null)[] = [];
    server.use(
      http.get('*/me/crawl/interests', ({ request }) => {
        const cursor = new URL(request.url).searchParams.get('cursor');
        cursors.push(cursor);
        return cursor === null
          ? json({ items: [interest('p1')], next_cursor: 'c2' })
          : json({ items: [interest('p9')], next_cursor: null });
      }),
    );
    open();
    fireEvent.click(await screen.findByTestId('user-crawl-load-more'));

    expect(await screen.findByTestId('user-crawl-contact-p9')).toBeInTheDocument();
    expect(cursors).toEqual([null, 'c2']);
    expect(screen.queryByTestId('user-crawl-load-more')).not.toBeInTheDocument();
  });

  it('highlights a column from ?depth= and removes the query', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const { router } = open('/crawl?depth=2');
    await screen.findByTestId('user-crawl-depth-column-2');

    await waitFor(() => expect(router.state.location.search).toBe('?depth=2'));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(3100);
    });
    expect(router.state.location.search).toBe('');
  });
});

describe('CrawlPage jobs and drawer', () => {
  it('opens the drawer from a row and closes it with Escape', async () => {
    const { router } = open();
    const row = await screen.findByTestId('user-crawl-job-j1');
    fireEvent.click(within(row).getByRole('button', { name: 'j1' }));

    const drawer = await screen.findByTestId('user-crawl-job-drawer');
    expect(drawer).toHaveAttribute('aria-modal', 'true');
    expect(router.state.location.search).toBe('?job=j1');
    expect(await screen.findByTestId('user-crawl-job-drawer-contact')).toHaveTextContent(
      'Contact p1',
    );
    expect(screen.getByTestId('user-crawl-job-drawer-facet')).toHaveTextContent('Photos');
    expect(screen.getByTestId('user-crawl-job-drawer-page')).toHaveTextContent('Page 3 of 9');
    expect(screen.getByTestId('user-crawl-job-drawer-attempt')).toHaveTextContent('2 of 5');
    expect(screen.getByTestId('user-crawl-job-drawer-state')).toHaveTextContent('Retrying');
    expect(screen.getByTestId('user-crawl-job-drawer-error')).toHaveTextContent(
      'Flickr asked us to slow down.',
    );
    expect(screen.getByTestId('user-crawl-job-drawer-error')).toHaveTextContent(
      'flickr.photos.search: 429',
    );
    expect(screen.getByTestId('user-crawl-job-drawer-attempts')).toHaveTextContent('Failed');
    expect(screen.getByTestId('user-crawl-job-drawer-times')).toBeInTheDocument();
    expect(drawer).toContainElement(document.activeElement as HTMLElement);

    fireEvent.keyDown(document, { key: 'Escape' });
    await waitFor(() =>
      expect(screen.queryByTestId('user-crawl-job-drawer')).not.toBeInTheDocument(),
    );
    expect(router.state.location.search).toBe('');
  });

  it('opens a job by deep link and closes it with the close button', async () => {
    open('/crawl?job=j1');

    expect(await screen.findByTestId('user-crawl-job-drawer-id')).toHaveTextContent('j1');
    fireEvent.click(screen.getByTestId('user-crawl-job-drawer-close'));
    await waitFor(() =>
      expect(screen.queryByTestId('user-crawl-job-drawer')).not.toBeInTheDocument(),
    );
  });

  it('opens a row by clicking anywhere on it', async () => {
    open();
    fireEvent.click(await screen.findByTestId('user-crawl-job-j1'));

    expect(await screen.findByTestId('user-crawl-job-drawer')).toBeInTheDocument();
  });

  it('keeps Tab inside the drawer', async () => {
    open('/crawl?job=j1');
    await screen.findByTestId('user-crawl-job-drawer-contact');
    const close = screen.getByTestId('user-crawl-job-drawer-close');
    const retry = screen.getByTestId('user-crawl-job-drawer-retry');
    retry.focus();
    fireEvent.keyDown(document, { key: 'Tab' });
    expect(close).toHaveFocus();
    fireEvent.keyDown(document, { key: 'Tab', shiftKey: true });
    expect(retry).toHaveFocus();
  });

  it('retries a failed job', async () => {
    let posted = 0;
    server.use(
      http.post('*/me/crawl/jobs/:id/retry', () => {
        posted += 1;
        return json({ ...jobFixture, state: 'pending' }, 202);
      }),
    );
    open('/crawl?job=j1');
    fireEvent.click(await screen.findByTestId('user-crawl-job-drawer-retry'));

    expect(await screen.findByText('Job queued again')).toBeInTheDocument();
    expect(posted).toBe(1);
    await waitFor(() =>
      expect(screen.getByTestId('user-crawl-job-drawer-state')).toHaveTextContent('Pending'),
    );
    expect(screen.queryByTestId('user-crawl-job-drawer-retry')).not.toBeInTheDocument();
  });

  it('explains a 409 on retry and refetches the job', async () => {
    let reads = 0;
    server.use(
      http.get('*/me/crawl/jobs/:id', () => {
        reads += 1;
        return json(jobFixture);
      }),
      http.post('*/me/crawl/jobs/:id/retry', () => fail(409, { code: 'job_not_retryable' })),
    );
    open('/crawl?job=j1');
    fireEvent.click(await screen.findByTestId('user-crawl-job-drawer-retry'));

    expect(await screen.findByText('This job cannot be retried now.')).toBeInTheDocument();
    await waitFor(() => expect(reads).toBeGreaterThanOrEqual(2));
  });

  it('reports another retry failure', async () => {
    server.use(http.post('*/me/crawl/jobs/:id/retry', () => fail(500)));
    open('/crawl?job=j1');
    fireEvent.click(await screen.findByTestId('user-crawl-job-drawer-retry'));

    expect(await screen.findByText('That did not work. Nothing changed.')).toBeInTheDocument();
  });

  it('shows the gone message for a missing job', async () => {
    server.use(http.get('*/me/crawl/jobs/:id', () => fail(404)));
    open('/crawl?job=zzz');

    expect(await screen.findByTestId('user-crawl-job-drawer-gone')).toHaveTextContent(
      'This job finished and left the list.',
    );
  });

  it('offers a retry when the job cannot load', async () => {
    let fails = true;
    server.use(
      http.get('*/me/crawl/jobs/:id', () =>
        fails
          ? fail(500)
          : json({
              ...jobFixture,
              state: 'running',
              error: null,
              attempts: [],
              next_retry_at: null,
              page: null,
            }),
      ),
    );
    open('/crawl?job=j1');
    const reload = await screen.findByTestId('user-crawl-job-drawer-reload');
    expect(screen.getByText('The job could not load.')).toBeInTheDocument();
    fails = false;
    fireEvent.click(reload);

    expect(await screen.findByTestId('user-crawl-job-drawer-contact')).toBeInTheDocument();
    expect(screen.getByText('The first attempt is running.')).toBeInTheDocument();
    expect(screen.queryByTestId('user-crawl-job-drawer-retry')).not.toBeInTheDocument();
  });

  it('loads more jobs and shows an empty list message', async () => {
    const limits: string[] = [];
    server.use(
      http.get('*/me/crawl/jobs', ({ request }) => {
        const url = new URL(request.url);
        limits.push(`${url.searchParams.get('limit')}:${url.searchParams.get('cursor') ?? ''}`);
        return url.searchParams.get('cursor') === null
          ? json({ items: [{ ...jobFixture, id: 'a', state: 'running' }], next_cursor: 'n1' })
          : json({
              items: [{ ...jobFixture, id: 'b', state: 'done', facet: 'mystery' }],
              next_cursor: null,
            });
      }),
    );
    open();
    fireEvent.click(await screen.findByTestId('user-crawl-jobs-more'));

    expect(await screen.findByTestId('user-crawl-job-b')).toHaveTextContent('mystery');
    expect(limits).toEqual(['10:', '50:n1']);
    expect(screen.queryByTestId('user-crawl-jobs-more')).not.toBeInTheDocument();

    server.use(http.get('*/me/crawl/jobs', () => json({ items: [], next_cursor: null })));
  });

  it('shows an empty jobs panel', async () => {
    server.use(http.get('*/me/crawl/jobs', () => json({ items: [], next_cursor: null })));
    open();

    expect(await screen.findByText('No jobs are waiting or running.')).toBeInTheDocument();
  });
});

describe('CrawlPage states', () => {
  it('shows the empty state with the account call to action', async () => {
    server.use(http.get('*/me/crawl/interests', () => json({ items: [], next_cursor: null })));
    open();

    expect(await screen.findByTestId('user-crawl-empty')).toHaveTextContent('Nothing to crawl yet');
    expect(screen.getByTestId('user-crawl-empty-cta')).toHaveAttribute('href', '/accounts');
    expect(screen.getByTestId('user-crawl-depth-card')).toBeInTheDocument();
    expect(screen.queryByTestId('user-crawl-lanes-card')).not.toBeInTheDocument();
    expect(screen.queryByTestId('user-crawl-depths')).not.toBeInTheDocument();
    expect(screen.queryByTestId('user-crawl-jobs')).not.toBeInTheDocument();
  });

  it('asks for a key in the empty state when there is none', async () => {
    queueWith({ state: 'no_key' });
    server.use(http.get('*/me/crawl/interests', () => json({ items: [], next_cursor: null })));
    open();

    expect(await screen.findByTestId('user-crawl-empty-cta')).toHaveAttribute(
      'href',
      '/keys?add=1',
    );
    expect(screen.getByTestId('user-crawl-empty-cta')).toHaveTextContent('Add API key');
  });

  it('shows the error state when every request fails and retries them', async () => {
    let healthy = false;
    const paths = ['queue', 'settings', 'depths', 'summary', 'interests', 'jobs'];
    server.use(
      ...paths.map((path) =>
        http.get(`*/me/crawl/${path}`, ({ request }) => {
          if (!healthy) return fail(500);
          void request;
          return undefined;
        }),
      ),
    );
    open();

    expect(await screen.findByTestId('user-crawl-error')).toHaveTextContent('Crawl could not load');
    healthy = true;
    fireEvent.click(screen.getByTestId('user-crawl-error-retry'));

    expect(await screen.findByTestId('user-crawl-contact-p1')).toBeInTheDocument();
    expect(screen.queryByTestId('user-crawl-error')).not.toBeInTheDocument();
  });

  it('shows the forbidden state on a first 403', async () => {
    const paths = ['queue', 'settings', 'depths', 'summary', 'interests', 'jobs'];
    server.use(
      ...paths.map((path) =>
        http.get(`*/me/crawl/${path}`, () => fail(403, { code: 'forbidden' })),
      ),
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
      http.get('*/me/crawl/depths', () => (healthy ? json({ items: depthsFixture }) : fail(500))),
      http.get('*/me/crawl/summary', () => fail(500)),
      http.get('*/me/crawl/settings', () => fail(500)),
      http.get('*/me/crawl/jobs', () => fail(500)),
      http.get('*/me/crawl/interests', () => fail(500)),
    );
    open();

    const section = await screen.findByTestId('user-crawl-section-error-depths');
    expect(section).toHaveTextContent('This section could not load.');
    expect(screen.getByTestId('user-crawl-section-error-contacts')).toBeInTheDocument();
    expect(screen.getByTestId('user-crawl-section-error-jobs')).toBeInTheDocument();
    expect(screen.getByTestId('user-crawl-section-error-summary')).toBeInTheDocument();
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

  it('treats a 200 answer that is not the expected JSON as a failure', async () => {
    server.use(
      http.get('*/me/crawl/queue', () => new HttpResponse('<html></html>', { status: 200 })),
    );
    open();

    expect(await screen.findByTestId('user-crawl-stale-banner')).toBeInTheDocument();
    expect(await screen.findByTestId('user-crawl-contact-p1')).toBeInTheDocument();
  });

  it('shows the skeleton after the delay while the first answers are pending', async () => {
    let release: () => void = () => undefined;
    const gate = new Promise<void>((resolve) => {
      release = resolve;
    });
    const paths = ['queue', 'settings', 'depths', 'summary', 'interests', 'jobs'];
    server.use(
      ...paths.map((path) =>
        http.get(`*/me/crawl/${path}`, async () => {
          await gate;
          return undefined;
        }),
      ),
    );
    open();

    expect(await screen.findByTestId('user-crawl-skeleton')).toBeInTheDocument();
    expect(screen.getByText('Loading crawl…')).toHaveAttribute('role', 'status');
    expect(screen.getByTestId('user-crawl-title')).toBeInTheDocument();
    release();
    expect(await screen.findByTestId('user-crawl-contact-p1')).toBeInTheDocument();
  });
});

describe('CrawlPage live updates', () => {
  it('applies queue, progress, job and settings events from the stream', async () => {
    vi.stubGlobal('EventSource', FakeSource);
    open();
    await screen.findByTestId('user-crawl-contact-p1');
    await screen.findByTestId('user-crawl-job-j1');
    FakeSource.last?.open();

    FakeSource.last?.emit('crawl.queue', {
      ...queueFixture,
      lanes: [
        { ...queueFixture.lanes[0], backlog_calls: 10, eta_hours: 0.5, position: 1 },
        queueFixture.lanes[1],
      ],
    });
    await waitFor(() =>
      expect(screen.getByTestId('user-crawl-lane-position-person')).toHaveTextContent('#1'),
    );
    expect(screen.getByTestId('user-crawl-lane-eta-person')).toHaveTextContent('~30 min');

    FakeSource.last?.emit('crawl.progress', {
      person_id: 'p1',
      status: 'done',
      photos_crawled: 200,
      sizes_percent: 100,
    });
    await waitFor(() =>
      expect(screen.getByTestId('user-crawl-contact-status-p1')).toHaveTextContent('Done'),
    );

    FakeSource.last?.emit('crawl.job', { ...jobFixture, state: 'running' });
    await waitFor(() =>
      expect(screen.getByTestId('user-crawl-job-j1')).toHaveTextContent('Running'),
    );
    FakeSource.last?.emit('crawl.job', { ...jobFixture, state: 'done' });
    await waitFor(() => expect(screen.queryByTestId('user-crawl-job-j1')).not.toBeInTheDocument());

    FakeSource.last?.emit('crawl.settings', { depth: 3, paused: true });
    await waitFor(() =>
      expect(screen.getByTestId('user-crawl-pause-all')).toHaveTextContent('Resume crawl'),
    );
    expect(screen.getByTestId('user-crawl-depth-value')).toHaveTextContent('Depth 3');
    FakeSource.last?.emit('crawl.unknown', {});
  });

  it('refetches the job list when an unknown job arrives and updates an open drawer', async () => {
    vi.stubGlobal('EventSource', FakeSource);
    let reads = 0;
    server.use(
      http.get('*/me/crawl/jobs', () => {
        reads += 1;
        return json({ items: [], next_cursor: null });
      }),
    );
    open('/crawl?job=j1');
    await screen.findByTestId('user-crawl-job-drawer-state');
    FakeSource.last?.open();
    const before = reads;
    FakeSource.last?.emit('crawl.job', { ...jobFixture, state: 'running' });

    await waitFor(() => expect(reads).toBeGreaterThan(before));
    expect(screen.getByTestId('user-crawl-job-drawer-state')).toHaveTextContent('Running');
  });

  it('ignores malformed events', async () => {
    vi.stubGlobal('EventSource', FakeSource);
    open();
    await screen.findByTestId('user-crawl-contact-p1');
    FakeSource.last?.open();
    act(() => {
      FakeSource.last?.listeners.get('crawl.queue')?.[0]?.({ data: '{nope' });
    });

    expect(screen.getByTestId('user-crawl-lane-eta-person')).toHaveTextContent('~3 h 20 min');
  });

  it('falls back to polling the queue when the stream is not open', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    let queueReads = 0;
    server.use(
      http.get('*/me/crawl/queue', () => {
        queueReads += 1;
        return json(queueFixture);
      }),
    );
    open();
    await screen.findByTestId('user-crawl-lane-eta-person');
    const first = queueReads;
    await act(async () => {
      await vi.advanceTimersByTimeAsync(15_500);
    });

    await waitFor(() => expect(queueReads).toBeGreaterThan(first));
  });

  it('shows the stale banner when polling stops answering', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    let healthy = true;
    server.use(http.get('*/me/crawl/queue', () => (healthy ? json(queueFixture) : fail(500))));
    open();
    await screen.findByTestId('user-crawl-lane-eta-person');
    healthy = false;
    await act(async () => {
      await vi.advanceTimersByTimeAsync(16_000);
    });

    expect(await screen.findByTestId('user-crawl-stale-banner')).toBeInTheDocument();
    expect(screen.getByTestId('user-crawl-lane-eta-person')).toHaveTextContent('~3 h 20 min');
  });

  it('closes the stream when the browser goes offline and catches up after', async () => {
    vi.stubGlobal('EventSource', FakeSource);
    open();
    await screen.findByTestId('user-crawl-contact-p1');
    const first = FakeSource.last;
    first?.open();
    act(() => {
      Object.defineProperty(navigator, 'onLine', { configurable: true, value: false });
      window.dispatchEvent(new Event('offline'));
    });
    expect(first?.closed).toBe(true);
    act(() => {
      Object.defineProperty(navigator, 'onLine', { configurable: true, value: true });
      window.dispatchEvent(new Event('online'));
    });

    await waitFor(() => expect(FakeSource.last).not.toBe(first));
    FakeSource.last?.onerror?.();
  });
});
