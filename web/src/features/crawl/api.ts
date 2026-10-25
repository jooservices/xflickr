import { ApiShapeError } from '../../shared/api/mutator';
import {
  getMeCrawl,
  getMeCrawlQueue,
  getMeCrawlSettings,
  updateMeCrawlSettings,
} from '../../shared/api/gen/crawl/crawl';
import type { CrawlQueue, CrawlSettings, CrawlStatus } from '../../shared/api/gen/models';

/**
 * Crawl screen calls, all through the generated client: the queue (`GET /me/crawl/queue`), the
 * progress by depth (`GET /me/crawl`) and the depth setting (`/me/crawl/settings`).
 */

export type Lane = 'person' | 'sizes';
export type { CrawlQueue, CrawlSettings, CrawlStatus };

/** A failed call: `status` is 0 for a network failure or timeout. */
export class CrawlApiError extends Error {
  readonly status: number;
  readonly code?: string;
  readonly details?: Record<string, unknown>;

  constructor(status: number, code?: string, details?: Record<string, unknown>) {
    super(`Crawl request failed with ${String(status)}`);
    this.status = status;
    this.code = code;
    this.details = details;
  }
}

export const REQUEST_TIMEOUT_MS = 10_000;

function timeoutSignal(signal?: AbortSignal): AbortSignal {
  const timeout = AbortSignal.timeout(REQUEST_TIMEOUT_MS);
  return signal === undefined ? timeout : AbortSignal.any([signal, timeout]);
}

/** Runs one generated-client call; any answer but 200 is a failure, a network error has status 0. */
async function ok<T>(
  call: (options: RequestInit) => Promise<{ status: number; data: unknown }>,
  signal?: AbortSignal,
): Promise<T> {
  let response: { status: number; data: unknown };
  try {
    response = await call({ signal: timeoutSignal(signal) });
  } catch (error) {
    if (signal?.aborted === true) throw error;
    throw new CrawlApiError(error instanceof ApiShapeError ? 502 : 0);
  }
  if (response.status !== 200) {
    const body = response.data as { code?: string; details?: Record<string, unknown> };
    throw new CrawlApiError(response.status, body.code, body.details);
  }
  return response.data as T;
}

export const loadQueue = (signal?: AbortSignal) => ok<CrawlQueue>(getMeCrawlQueue, signal);
export const loadSettings = (signal?: AbortSignal) => ok<CrawlSettings>(getMeCrawlSettings, signal);
export const loadStatus = (signal?: AbortSignal) => ok<CrawlStatus>(getMeCrawl, signal);

/** Stores the depth; a depth above the administrator cap answers 422 `depth_above_cap`. */
export const saveDepth = (depth: number) =>
  ok<CrawlSettings>((options) => updateMeCrawlSettings({ depth }, options));
