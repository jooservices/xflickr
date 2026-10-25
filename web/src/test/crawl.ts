import { http, HttpResponse } from 'msw/http';
import type { CrawlQueue, CrawlSettings, CrawlStatus } from '../features/crawl';

export const queueFixture: CrawlQueue = {
  lanes: [
    { lane: 'person', backlog_calls: 1200, effective_calls_per_hour: 360, eta_hours: 3.34 },
    { lane: 'sizes', backlog_calls: 0, effective_calls_per_hour: 360, eta_hours: null },
  ],
};

export const settingsFixture: CrawlSettings = { depth: 2, depth_default: 2, depth_max: 4 };

export const statusFixture: CrawlStatus = {
  depths: [
    { depth: 0, contacts: 1 },
    { depth: 1, contacts: 40 },
    { depth: 2, contacts: 300 },
    { depth: 3, contacts: 12 },
  ],
  facets: [{ facet: 'photos', total: 341, done: 130, active: 20, failed: 2 }],
  lanes: [{ lane: 'person', pending: 10, running: 2, waiting: 1, paused: 0 }],
};

/** A healthy crawl served by the real contract paths. Tests override single endpoints. */
export const crawlHandlers = [
  http.get('*/me/crawl/queue', () => HttpResponse.json(queueFixture)),
  http.get('*/me/crawl/settings', () => HttpResponse.json(settingsFixture)),
  http.get('*/me/crawl', () => HttpResponse.json(statusFixture)),
];
