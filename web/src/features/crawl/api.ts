import { request } from '../auth/api';

/**
 * Crawl screen calls. `GET /me/crawl/queue` and the pause/resume/star/select calls exist in
 * OpenAPI, but the screen needs the richer shapes requested in docs/ui/views/user-crawl.md
 * (Data and API). Those paths are contract requests, so everything goes through the shared
 * request helper (CSRF header, session-expiry signal) and a missing endpoint is a plain failure.
 */

export type Lane = 'person' | 'sizes';
export type RunState = 'running' | 'waiting_quota' | 'key_exhausted' | 'no_key' | 'paused';
export type InterestStatus = 'done' | 'crawling' | 'queued' | 'paused' | 'error';
export type JobState = 'pending' | 'running' | 'waiting' | 'retry' | 'failed' | 'done';
export type JobPriority = 'selected' | 'starred' | 'depth' | 'normal';
export type InterestFilter = 'all' | 'starred' | 'selected' | 'crawling' | 'attention';

export type LaneQueue = {
  lane: Lane;
  backlog_calls: number;
  effective_calls_per_hour: number;
  eta_hours: number | null;
  position?: number | null;
  done_calls?: number;
};

export type CrawlQueue = {
  lanes: LaneQueue[];
  state: RunState;
  resets_at: string | null;
  server_time: string;
  exhausted_keys: { id: number | string; label: string }[];
};

export type CrawlSettings = {
  depth: number;
  default_depth: number;
  max_depth: number;
  paused: boolean;
};

export type DepthRow = {
  depth: number;
  contacts_total: number;
  done: number;
  crawling: number;
  waiting: number;
  error: number;
  beyond_setting: boolean;
};

export type Interest = {
  person_id: string;
  nsid: string;
  name: string;
  avatar_url: string | null;
  depth: number;
  starred: boolean;
  selected_until: string | null;
  paused: boolean;
  status: InterestStatus;
  photos_crawled: number;
  photos_total: number;
  sizes_percent: number;
  last_check_at: string | null;
};

export type InterestPage = { items: Interest[]; next_cursor: string | null };

export type CrawlSummary = {
  recrawl_interval_hours: number;
  contacts_done: number;
  contacts_total: number;
  calls_saved_week: number;
};

export type Job = {
  id: string;
  person_id: string;
  contact_name: string;
  facet: string;
  lane: Lane;
  state: JobState;
  attempt: number;
  max_attempts: number;
  page: number | null;
  pages: number | null;
  created_at: string;
};

export type JobPage = { items: Job[]; next_cursor: string | null };

export type JobAttempt = { n: number; at: string; result: string; duration_ms: number };

export type JobDetail = Job & {
  priority: JobPriority;
  key_label: string | null;
  calls: number;
  records: number;
  error: { code: string; message: string } | null;
  started_at: string | null;
  finished_at: string | null;
  next_retry_at: string | null;
  attempts: JobAttempt[];
};

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

type ErrorBody = { code?: string; details?: Record<string, unknown> };

async function call<T>(
  path: string,
  method: string,
  body?: unknown,
  signal?: AbortSignal,
): Promise<{ status: number; data: T }> {
  const timeout = AbortSignal.timeout(REQUEST_TIMEOUT_MS);
  try {
    const response = await request<T & ErrorBody>(
      path,
      method,
      body,
      signal === undefined ? timeout : AbortSignal.any([signal, timeout]),
    );
    if (response.status < 200 || response.status >= 300) {
      throw new CrawlApiError(response.status, response.data.code, response.data.details);
    }
    return response;
  } catch (error) {
    if (error instanceof CrawlApiError) throw error;
    if (signal?.aborted === true) throw error;
    throw new CrawlApiError(0);
  }
}

/** Sends an unsafe call; the shared fetch wrapper handles CSRF refresh and retry. */
async function send<T>(path: string, method: string, body?: unknown): Promise<T> {
  return (await call<T>(path, method, body)).data;
}

const get = async <T>(path: string, signal?: AbortSignal): Promise<T> =>
  (await call<T>(path, 'GET', undefined, signal)).data;

const query = (params: Record<string, string | number | undefined>): string => {
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== '') search.set(key, String(value));
  }
  const text = search.toString();
  return text === '' ? '' : `?${text}`;
};

/** A 2xx answer that is not the expected JSON (for example an HTML fallback page) is a failure. */
async function getShaped<T>(
  path: string,
  fits: (data: T) => boolean,
  signal?: AbortSignal,
): Promise<T> {
  const data = await get<T>(path, signal);
  if (!fits(data)) throw new CrawlApiError(502);
  return data;
}

const isList = (value: unknown): boolean => Array.isArray(value);

export const loadQueue = (signal?: AbortSignal) =>
  getShaped<CrawlQueue>('/me/crawl/queue', (data) => isList(data.lanes), signal);
export const loadSettings = (signal?: AbortSignal) =>
  getShaped<CrawlSettings>(
    '/me/crawl/settings',
    (data) => typeof data.depth === 'number' && typeof data.max_depth === 'number',
    signal,
  );
export const loadDepths = async (signal?: AbortSignal) =>
  (await getShaped<{ items: DepthRow[] }>('/me/crawl/depths', (data) => isList(data.items), signal))
    .items;
export const loadSummary = (signal?: AbortSignal) =>
  getShaped<CrawlSummary>(
    '/me/crawl/summary',
    (data) => typeof data.contacts_total === 'number',
    signal,
  );

export const saveSettings = (body: { depth?: number; paused?: boolean }) =>
  send<CrawlSettings>('/me/crawl/settings', 'PUT', body);

export const CONTACT_PAGE_SIZE = 50;

export function loadInterests(
  params: { filter: InterestFilter; depth?: number; cursor?: string },
  signal?: AbortSignal,
) {
  return getShaped<InterestPage>(
    `/me/crawl/interests${query({
      filter: params.filter,
      depth: params.depth,
      cursor: params.cursor,
      limit: CONTACT_PAGE_SIZE,
    })}`,
    (data) => isList(data.items),
    signal,
  );
}

export const updateInterest = (
  personId: string,
  body: { starred?: boolean; selected?: boolean; paused?: boolean },
) => send<Interest>(`/me/crawl/interests/${encodeURIComponent(personId)}`, 'PUT', body);

export const JOBS_FIRST_PAGE = 10;
export const JOBS_NEXT_PAGE = 50;

export function loadJobs(params: { limit: number; cursor?: string }, signal?: AbortSignal) {
  return getShaped<JobPage>(
    `/me/crawl/jobs${query({ state: 'active', limit: params.limit, cursor: params.cursor })}`,
    (data) => isList(data.items),
    signal,
  );
}

export const loadJob = (id: string, signal?: AbortSignal) =>
  getShaped<JobDetail>(
    `/me/crawl/jobs/${encodeURIComponent(id)}`,
    (data) => typeof data.id === 'string',
    signal,
  );

export const retryJob = (id: string) =>
  send<Job>(`/me/crawl/jobs/${encodeURIComponent(id)}/retry`, 'POST');
