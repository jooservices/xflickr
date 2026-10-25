import { useQueryClient } from '@tanstack/react-query';
import type { InfiniteData, QueryClient } from '@tanstack/react-query';
import { useEffect, useRef, useState } from 'react';
import type {
  CrawlQueue,
  CrawlSettings,
  Interest,
  InterestPage,
  Job,
  JobDetail,
  JobPage,
} from './api';
import { crawlKeys } from './queries';

export const STREAM_URL = '/api/v1/events/stream?channel=crawl.user';

type Progress = Pick<Interest, 'person_id' | 'status' | 'photos_crawled' | 'sizes_percent'>;

function mapPages<P extends { items: unknown[] }>(
  data: InfiniteData<P> | undefined,
  map: (items: P['items']) => P['items'],
): InfiniteData<P> | undefined {
  return data === undefined
    ? data
    : { ...data, pages: data.pages.map((page) => ({ ...page, items: map(page.items) })) };
}

/** Patches one contact in place so rows never re-sort when a status changes. */
export function applyProgress(client: QueryClient, event: Progress): void {
  client.setQueriesData<InfiniteData<InterestPage>>({ queryKey: crawlKeys.interests }, (data) =>
    mapPages(data, (items) =>
      (items as Interest[]).map((item) =>
        item.person_id === event.person_id
          ? {
              ...item,
              status: event.status,
              photos_crawled: event.photos_crawled,
              sizes_percent: event.sizes_percent,
            }
          : item,
      ),
    ),
  );
  void client.invalidateQueries({ queryKey: crawlKeys.depths });
}

/** Updates the job list and an open drawer; a job that is new to the list triggers a refetch. */
export function applyJob(client: QueryClient, job: Job): void {
  let found = false;
  client.setQueryData<InfiniteData<JobPage>>(crawlKeys.jobs, (data) =>
    mapPages(data, (items) =>
      (items as Job[]).flatMap((item) => {
        if (item.id !== job.id) return [item];
        found = true;
        return job.state === 'done' ? [] : [{ ...item, ...job }];
      }),
    ),
  );
  if (!found && job.state !== 'done') void client.invalidateQueries({ queryKey: crawlKeys.jobs });
  client.setQueryData<JobDetail>(crawlKeys.job(job.id), (detail) =>
    detail === undefined ? detail : { ...detail, ...job },
  );
}

type StreamEvent = { type: string; data: string };

function handle(client: QueryClient, event: StreamEvent): void {
  let payload: unknown;
  try {
    payload = JSON.parse(event.data);
  } catch {
    return;
  }
  switch (event.type) {
    case 'crawl.queue':
      client.setQueryData<CrawlQueue>(crawlKeys.queue, payload as CrawlQueue);
      break;
    case 'crawl.progress':
      applyProgress(client, payload as Progress);
      break;
    case 'crawl.job':
      applyJob(client, payload as Job);
      break;
    case 'crawl.settings':
      client.setQueryData<CrawlSettings>(crawlKeys.settings, (current) =>
        current === undefined ? current : { ...current, ...(payload as Partial<CrawlSettings>) },
      );
      break;
  }
}

const EVENTS = ['crawl.queue', 'crawl.progress', 'crawl.job', 'crawl.settings'] as const;

/**
 * Opens the `crawl.user` channel of the event stream and applies its events to the
 * query cache. Returns whether the stream is open; while it is not, the page polls.
 * While `enabled` is false the stream is closed and the cache is refetched on resume.
 */
export function useCrawlStream(enabled: boolean): boolean {
  const client = useQueryClient();
  const [open, setOpen] = useState(false);
  const started = useRef(false);

  useEffect(() => {
    if (!enabled || typeof EventSource === 'undefined') return undefined;
    // Events were not applied while the stream was closed, so catch up once.
    if (started.current) void client.invalidateQueries({ queryKey: crawlKeys.all });
    started.current = true;
    const source = new EventSource(STREAM_URL, { withCredentials: true });
    source.onopen = () => {
      setOpen(true);
    };
    source.onerror = () => {
      setOpen(false);
    };
    for (const type of EVENTS) {
      source.addEventListener(type, (event) => {
        handle(client, { type, data: String((event as MessageEvent).data) });
      });
    }
    return () => {
      source.close();
      setOpen(false);
    };
  }, [client, enabled]);

  return enabled && open;
}
