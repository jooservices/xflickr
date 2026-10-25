import { useQuery } from '@tanstack/react-query';
import { loadQueue, loadSettings, loadStatus } from './api';

export const crawlKeys = {
  all: ['crawl'] as const,
  queue: ['crawl', 'queue'] as const,
  settings: ['crawl', 'settings'] as const,
  status: ['crawl', 'status'] as const,
};

/** Polling intervals (docs/ui/views/user-crawl.md, Live updates): the contract has no event stream yet. */
export const POLL_QUEUE_MS = 15_000;
export const POLL_STATUS_MS = 30_000;
const POLL_QUEUE_MAX_MS = 60_000;

const noRetry = { retry: false, refetchIntervalInBackground: false } as const;

export const useQueueQuery = () =>
  useQuery({
    queryKey: crawlKeys.queue,
    queryFn: ({ signal }) => loadQueue(signal),
    refetchInterval: (query) =>
      Math.min(POLL_QUEUE_MS * 2 ** query.state.fetchFailureCount, POLL_QUEUE_MAX_MS),
    ...noRetry,
  });

export const useSettingsQuery = () =>
  useQuery({
    queryKey: crawlKeys.settings,
    queryFn: ({ signal }) => loadSettings(signal),
    ...noRetry,
  });

export const useStatusQuery = () =>
  useQuery({
    queryKey: crawlKeys.status,
    queryFn: ({ signal }) => loadStatus(signal),
    refetchInterval: POLL_STATUS_MS,
    ...noRetry,
  });
