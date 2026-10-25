import { keepPreviousData, useInfiniteQuery, useQuery } from '@tanstack/react-query';
import {
  JOBS_FIRST_PAGE,
  JOBS_NEXT_PAGE,
  loadDepths,
  loadInterests,
  loadJob,
  loadJobs,
  loadQueue,
  loadSettings,
  loadSummary,
} from './api';
import type { InterestFilter } from './api';

export const crawlKeys = {
  all: ['crawl'] as const,
  queue: ['crawl', 'queue'] as const,
  settings: ['crawl', 'settings'] as const,
  depths: ['crawl', 'depths'] as const,
  summary: ['crawl', 'summary'] as const,
  interests: ['crawl', 'interests'] as const,
  jobs: ['crawl', 'jobs'] as const,
  job: (id: string) => ['crawl', 'job', id] as const,
};

/** Polling intervals of the fallback (docs/ui/views/user-crawl.md, Live updates). */
export const POLL_QUEUE_MS = 15_000;
export const POLL_JOBS_MS = 10_000;
export const POLL_LISTS_MS = 30_000;
const POLL_QUEUE_MAX_MS = 60_000;

/** `base` while polling works; doubled after each failed poll up to 60 s. */
function interval(polling: boolean, base: number, backoff = false) {
  return (query: { state: { fetchFailureCount: number } }): number | false => {
    if (!polling) return false;
    if (!backoff) return base;
    return Math.min(base * 2 ** query.state.fetchFailureCount, POLL_QUEUE_MAX_MS);
  };
}

const noRetry = { retry: false, refetchIntervalInBackground: false } as const;

export const useQueueQuery = (polling: boolean) =>
  useQuery({
    queryKey: crawlKeys.queue,
    queryFn: ({ signal }) => loadQueue(signal),
    refetchInterval: interval(polling, POLL_QUEUE_MS, true),
    ...noRetry,
  });

export const useSettingsQuery = () =>
  useQuery({
    queryKey: crawlKeys.settings,
    queryFn: ({ signal }) => loadSettings(signal),
    ...noRetry,
  });

export const useDepthsQuery = (polling: boolean, enabled = true) =>
  useQuery({
    queryKey: crawlKeys.depths,
    queryFn: ({ signal }) => loadDepths(signal),
    refetchInterval: interval(polling, POLL_LISTS_MS),
    enabled,
    ...noRetry,
  });

export const useSummaryQuery = (enabled = true) =>
  useQuery({
    queryKey: crawlKeys.summary,
    queryFn: ({ signal }) => loadSummary(signal),
    enabled,
    ...noRetry,
  });

export const useInterestsQuery = (
  filter: InterestFilter,
  depth: number | undefined,
  polling: boolean,
) =>
  useInfiniteQuery({
    queryKey: [...crawlKeys.interests, filter, depth ?? 'any'],
    queryFn: ({ pageParam, signal }) =>
      loadInterests({ filter, depth, cursor: pageParam === '' ? undefined : pageParam }, signal),
    initialPageParam: '',
    getNextPageParam: (last) => last.next_cursor ?? undefined,
    placeholderData: keepPreviousData,
    refetchInterval: interval(polling, POLL_LISTS_MS),
    ...noRetry,
  });

export const useJobsQuery = (polling: boolean, enabled = true) =>
  useInfiniteQuery({
    queryKey: crawlKeys.jobs,
    queryFn: ({ pageParam, signal }) =>
      loadJobs(
        pageParam === ''
          ? { limit: JOBS_FIRST_PAGE }
          : { limit: JOBS_NEXT_PAGE, cursor: pageParam },
        signal,
      ),
    initialPageParam: '',
    getNextPageParam: (last) => last.next_cursor ?? undefined,
    refetchInterval: interval(polling, POLL_JOBS_MS),
    enabled,
    ...noRetry,
  });

export const useJobQuery = (id: string | null, polling: boolean) =>
  useQuery({
    queryKey: crawlKeys.job(id ?? ''),
    queryFn: ({ signal }) => loadJob(id ?? '', signal),
    enabled: id !== null,
    refetchInterval: interval(polling, POLL_JOBS_MS),
    ...noRetry,
  });
