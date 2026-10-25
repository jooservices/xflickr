import { http, HttpResponse } from 'msw/http';
import type { CrawlQueue, CrawlSettings, DepthRow, Interest, JobDetail } from '../features/crawl';

export const queueFixture: CrawlQueue = {
  state: 'running',
  resets_at: null,
  server_time: '2026-10-25T10:00:00Z',
  exhausted_keys: [],
  lanes: [
    {
      lane: 'person',
      backlog_calls: 1200,
      effective_calls_per_hour: 360,
      eta_hours: 3.34,
      position: 4,
      done_calls: 800,
    },
    {
      lane: 'sizes',
      backlog_calls: 0,
      effective_calls_per_hour: 360,
      eta_hours: null,
      position: null,
      done_calls: 50,
    },
  ],
};

export const settingsFixture: CrawlSettings = {
  depth: 2,
  default_depth: 2,
  max_depth: 4,
  paused: false,
};

export const depthsFixture: DepthRow[] = [
  {
    depth: 0,
    contacts_total: 1,
    done: 1,
    crawling: 0,
    waiting: 0,
    error: 0,
    beyond_setting: false,
  },
  {
    depth: 1,
    contacts_total: 40,
    done: 30,
    crawling: 5,
    waiting: 4,
    error: 1,
    beyond_setting: false,
  },
  {
    depth: 2,
    contacts_total: 300,
    done: 100,
    crawling: 20,
    waiting: 170,
    error: 10,
    beyond_setting: false,
  },
  {
    depth: 3,
    contacts_total: 12,
    done: 12,
    crawling: 0,
    waiting: 0,
    error: 0,
    beyond_setting: true,
  },
];

export const interest = (id: string, patch: Partial<Interest> = {}): Interest => ({
  person_id: id,
  nsid: `${id}@N01`,
  name: `Contact ${id}`,
  avatar_url: null,
  depth: 1,
  starred: false,
  selected_until: null,
  paused: false,
  status: 'crawling',
  photos_crawled: 50,
  photos_total: 200,
  sizes_percent: 25,
  last_check_at: '2026-10-25T09:30:00Z',
  ...patch,
});

export const jobFixture: JobDetail = {
  id: 'j1',
  person_id: 'p1',
  contact_name: 'Contact p1',
  facet: 'photos',
  lane: 'person',
  state: 'retry',
  attempt: 2,
  max_attempts: 5,
  page: 3,
  pages: 9,
  created_at: '2026-10-25T09:00:00Z',
  priority: 'starred',
  key_label: 'My Flickr key',
  calls: 14,
  records: 1200,
  error: { code: 'rate_limited', message: 'flickr.photos.search: 429' },
  started_at: '2026-10-25T09:05:00Z',
  finished_at: null,
  next_retry_at: '2026-10-25T10:05:00Z',
  attempts: [{ n: 1, at: '2026-10-25T09:05:00Z', result: 'failed', duration_ms: 1500 }],
};

/** A healthy crawl: two contacts, one active job. Tests override single endpoints. */
export const crawlHandlers = [
  http.get('*/me/crawl/queue', () => HttpResponse.json(queueFixture)),
  http.get('*/me/crawl/settings', () => HttpResponse.json(settingsFixture)),
  http.get('*/me/crawl/depths', () => HttpResponse.json({ items: depthsFixture })),
  http.get('*/me/crawl/summary', () =>
    HttpResponse.json({
      recrawl_interval_hours: 24,
      contacts_done: 143,
      contacts_total: 353,
      calls_saved_week: 5200,
    }),
  ),
  http.get('*/me/crawl/interests', () =>
    HttpResponse.json({
      items: [interest('p1', { starred: true }), interest('p2', { depth: 2 })],
      next_cursor: null,
    }),
  ),
  http.get('*/me/crawl/jobs', () =>
    HttpResponse.json({
      items: [{ ...jobFixture, attempts: undefined }],
      next_cursor: null,
    }),
  ),
  http.get('*/me/crawl/jobs/:id', () => HttpResponse.json(jobFixture)),
];
