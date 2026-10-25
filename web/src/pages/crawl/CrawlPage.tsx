import { useQueryClient } from '@tanstack/react-query';
import type { InfiniteData } from '@tanstack/react-query';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router';
import { Icon, useI18n, useLoadingTiming } from '../../shared';
import {
  CrawlApiError,
  crawlKeys,
  retryJob,
  saveSettings,
  updateInterest,
  useCrawlStream,
  useDepthsQuery,
  useInterestsQuery,
  useJobQuery,
  useJobsQuery,
  useQueueQuery,
  useSettingsQuery,
  useSummaryQuery,
} from '../../features/crawl';
import type {
  Interest,
  InterestFilter,
  InterestPage,
  Job,
  JobDetail,
  JobPage,
} from '../../features/crawl';
import { Contacts, FILTERS } from './Contacts';
import { DepthCard, LanesCard, PerDepth, RecrawlCard } from './Cards';
import { formatClock, useNow } from './format';
import { JobDrawer } from './JobDrawer';
import { Jobs } from './Jobs';
import { Banner, Countdown, SectionError, SkeletonBlock } from './parts';
import styles from './CrawlPage.module.css';

const STALE_AFTER_MS = 30_000;
const HIGHLIGHT_MS = 3000;

type Toast = { id: number; text: string; tone: 'ok' | 'bad' };

function useOnline(): boolean {
  const [online, setOnline] = useState(() => navigator.onLine);
  useEffect(() => {
    const update = () => {
      setOnline(navigator.onLine);
    };
    window.addEventListener('online', update);
    window.addEventListener('offline', update);
    return () => {
      window.removeEventListener('online', update);
      window.removeEventListener('offline', update);
    };
  }, []);
  return online;
}

const parseFilter = (value: string | null): InterestFilter =>
  FILTERS.find((name) => name === value) ?? 'all';

const status = (error: unknown): number | undefined =>
  error instanceof CrawlApiError ? error.status : undefined;

export function CrawlPage() {
  const { t, locale } = useI18n();
  const navigate = useNavigate();
  const client = useQueryClient();
  const [params, setParams] = useSearchParams();
  const online = useOnline();
  const streaming = useCrawlStream(online);
  const polling = !streaming;
  const now = useNow(true, 5000);

  const filter = parseFilter(params.get('filter'));
  const [depthFilter, setDepthFilter] = useState<number | undefined>(undefined);
  const jobId = params.get('job');
  const depthParam = params.get('depth');
  const [highlight, setHighlight] = useState<number | undefined>(undefined);

  const queue = useQueueQuery(polling);
  const settings = useSettingsQuery();
  const depths = useDepthsQuery(polling);
  const summary = useSummaryQuery();
  const interests = useInterestsQuery(filter, depthFilter, polling);
  const jobs = useJobsQuery(polling);
  const job = useJobQuery(jobId, polling);

  const [toasts, setToasts] = useState<Toast[]>([]);
  const nextToast = useRef(1);
  const [announcement, setAnnouncement] = useState('');
  const [pausing, setPausing] = useState(false);
  const [retrying, setRetrying] = useState(false);
  const [busy, setBusy] = useState<ReadonlySet<string>>(new Set());
  const inflight = useRef(new Set<string>());
  const opener = useRef<HTMLElement | null>(null);
  const headingRef = useRef<HTMLHeadingElement>(null);

  const toast = useCallback((text: string, tone: Toast['tone'] = 'ok') => {
    const id = nextToast.current;
    nextToast.current += 1;
    setToasts((current) => [...current, { id, text, tone }]);
    window.setTimeout(() => {
      setToasts((current) => current.filter((item) => item.id !== id));
    }, 2600);
  }, []);

  useEffect(() => {
    document.title = `${t('user.crawl.title')} · XFlickr`;
    headingRef.current?.focus();
  }, [t]);

  // Run state changes are announced once, never every job.
  const runState = queue.data?.state;
  const lastState = useRef(runState);
  useEffect(() => {
    if (
      runState !== undefined &&
      lastState.current !== undefined &&
      lastState.current !== runState
    ) {
      setAnnouncement(
        t('user.crawl.live.state-changed', { state: t(`user.crawl.state.${runState}`) }),
      );
    }
    lastState.current = runState;
  }, [runState, t]);

  // `?depth=<n>` scrolls to and highlights a column, then the query goes away.
  const depthRows = depths.data;
  useEffect(() => {
    if (depthParam === null || depthRows === undefined) return undefined;
    const depth = Number(depthParam);
    const apply = () => {
      setHighlight(Number.isInteger(depth) ? depth : undefined);
      document
        .querySelector(`[data-testid="user-crawl-depth-column-${String(depth)}"]`)
        ?.scrollIntoView?.({ block: 'nearest', inline: 'center' });
    };
    apply();
    const timer = window.setTimeout(() => {
      setHighlight(undefined);
      setParams(
        (current) => {
          const next = new URLSearchParams(current);
          next.delete('depth');
          return next;
        },
        { replace: true },
      );
    }, HIGHLIGHT_MS);
    return () => {
      window.clearTimeout(timer);
    };
  }, [depthParam, depthRows !== undefined, setParams]); // eslint-disable-line react-hooks/exhaustive-deps

  const core = [queue, settings, depths, summary, interests, jobs];
  const hasData = core.some((query) => query.data !== undefined);
  const forbidden = !hasData && core.some((query) => status(query.error) === 403);
  const allFailed = core.every((query) => query.isError);
  const allPending = core.every((query) => query.isPending);
  const anyPending = core.some((query) => query.isPending);
  const skeleton = useLoadingTiming(anyPending, 150);

  const retryAll = () => {
    for (const query of core) if (query.isError) void query.refetch();
  };

  const interestItems = useMemo(
    () => interests.data?.pages.flatMap((page) => page.items) ?? [],
    [interests.data],
  );
  const jobItems = useMemo(() => jobs.data?.pages.flatMap((page) => page.items) ?? [], [jobs.data]);

  const filterActive = filter !== 'all' || depthFilter !== undefined;
  const empty = interests.isSuccess && interestItems.length === 0 && !filterActive;

  const patchInterest = (personId: string, patch: (item: Interest) => Interest | null) => {
    client.setQueriesData<InfiniteData<InterestPage>>({ queryKey: crawlKeys.interests }, (data) =>
      data === undefined
        ? data
        : {
            ...data,
            pages: data.pages.map((page) => ({
              ...page,
              items: page.items.flatMap((item) => {
                if (item.person_id !== personId) return [item];
                const next = patch(item);
                return next === null ? [] : [next];
              }),
            })),
          },
    );
  };

  const setJobState = (id: string, state: Job['state']) => {
    client.setQueryData<JobDetail>(crawlKeys.job(id), (detail) =>
      detail === undefined ? detail : { ...detail, state },
    );
    client.setQueryData<InfiniteData<JobPage>>(crawlKeys.jobs, (data) =>
      data === undefined
        ? data
        : {
            ...data,
            pages: data.pages.map((page) => ({
              ...page,
              items: page.items.map((item) => (item.id === id ? { ...item, state } : item)),
            })),
          },
    );
  };

  const changeDepth = async (depth: number): Promise<boolean> => {
    try {
      const saved = await saveSettings({ depth });
      client.setQueryData(crawlKeys.settings, saved);
      toast(t('user.crawl.toast.depth', { n: saved.depth }));
      void client.invalidateQueries({ queryKey: crawlKeys.depths });
      return true;
    } catch (error) {
      if (
        error instanceof CrawlApiError &&
        error.status === 422 &&
        error.code === 'depth_above_cap'
      ) {
        const max = Number(error.details?.max_depth ?? settings.data?.max_depth ?? depth);
        toast(t('user.crawl.error.depth-cap', { max }), 'bad');
      } else {
        toast(t('user.crawl.error.action-failed'), 'bad');
      }
      return false;
    }
  };

  const paused = settings.data?.paused ?? queue.data?.state === 'paused';

  const togglePaused = async () => {
    if (pausing) return;
    const next = !paused;
    setPausing(true);
    try {
      const saved = await saveSettings({ paused: next });
      client.setQueryData(crawlKeys.settings, saved);
      void client.invalidateQueries({ queryKey: crawlKeys.queue });
      toast(t(next ? 'user.crawl.toast.paused' : 'user.crawl.toast.resumed'));
    } catch {
      toast(t('user.crawl.error.action-failed'), 'bad');
    }
    setPausing(false);
  };

  const runContactAction = async (
    item: Interest,
    body: { starred?: boolean; selected?: boolean; paused?: boolean },
    optimistic?: (current: Interest) => Interest,
    done?: () => void,
  ) => {
    const id = item.person_id;
    if (inflight.current.has(id)) return;
    inflight.current.add(id);
    setBusy(new Set(inflight.current));
    if (optimistic) patchInterest(id, optimistic);
    try {
      const saved = await updateInterest(id, body);
      patchInterest(id, (current) => ({ ...current, ...saved }));
      done?.();
    } catch (error) {
      if (status(error) === 404) {
        patchInterest(id, () => null);
      } else {
        if (optimistic) patchInterest(id, () => item);
        toast(t('user.crawl.error.action-failed'), 'bad');
      }
    }
    inflight.current.delete(id);
    setBusy(new Set(inflight.current));
  };

  const openJob = (id: string, element: HTMLElement) => {
    opener.current = element;
    setParams((current) => {
      const next = new URLSearchParams(current);
      next.set('job', id);
      return next;
    });
  };

  const closeJob = useCallback(() => {
    setParams(
      (current) => {
        const next = new URLSearchParams(current);
        next.delete('job');
        return next;
      },
      { replace: true },
    );
  }, [setParams]);

  const retryCurrent = async () => {
    if (jobId === null || retrying) return;
    setRetrying(true);
    try {
      await retryJob(jobId);
      setJobState(jobId, 'pending');
      toast(t('user.crawl.toast.retry'));
    } catch (error) {
      if (status(error) === 409) {
        toast(t('user.crawl.error.not-retryable'), 'bad');
        void job.refetch();
      } else {
        toast(t('user.crawl.error.action-failed'), 'bad');
      }
    }
    setRetrying(false);
  };

  const setFilter = (name: InterestFilter) => {
    setParams(
      (current) => {
        const next = new URLSearchParams(current);
        if (name === 'all') next.delete('filter');
        else next.set('filter', name);
        return next;
      },
      { replace: true },
    );
  };

  const clearFilters = () => {
    setFilter('all');
    setDepthFilter(undefined);
  };

  if (forbidden) {
    return (
      <div className={styles.page} data-testid="user-crawl-page">
        <div className={styles.block} data-testid="user-crawl-forbidden">
          <span className={styles.blockIcon}>
            <Icon name="lock" size={36} />
          </span>
          <h1 tabIndex={-1} ref={headingRef} data-testid="user-crawl-title">
            {t('user.crawl.forbidden.title')}
          </h1>
          <p>{t('user.crawl.forbidden.text')}</p>
          <button
            type="button"
            className={`${styles.btn} ${styles.primary}`}
            onClick={() => {
              void navigate('/');
            }}
            data-testid="user-crawl-forbidden-cta"
          >
            {t('user.crawl.forbidden.cta')}
          </button>
        </div>
      </div>
    );
  }

  const queueData = queue.data;
  const age = queue.dataUpdatedAt === 0 ? 0 : now - queue.dataUpdatedAt;
  const stale = queue.isError || (queueData !== undefined && !streaming && age > STALE_AFTER_MS);
  const state = queueData?.state;
  const keysLink = (testId: string, label: string, to: string) => (
    <Link to={to} className={styles.btn} data-testid={testId}>
      {label}
    </Link>
  );

  let banner = null;
  if (queueData !== undefined) {
    if (state === 'no_key') {
      banner = (
        <Banner
          tone="bad"
          role="alert"
          testId="user-crawl-banner-no-key"
          action={keysLink(
            'user-crawl-banner-action',
            t('user.crawl.banner.add-key'),
            '/keys?add=1',
          )}
        >
          {t('user.crawl.banner.no-key')}
        </Banner>
      );
    } else if (state === 'paused') {
      banner = (
        <Banner
          tone="info"
          role="status"
          testId="user-crawl-banner-paused"
          action={
            <button
              type="button"
              className={styles.btn}
              disabled={pausing}
              onClick={() => {
                void togglePaused();
              }}
              data-testid="user-crawl-banner-action"
            >
              {t('user.crawl.resume-all')}
            </button>
          }
        >
          {t('user.crawl.banner.paused')}
        </Banner>
      );
    } else if (state === 'waiting_quota') {
      banner = (
        <Banner
          tone="warn"
          role="status"
          testId="user-crawl-banner-waiting"
          action={keysLink('user-crawl-banner-action', t('user.crawl.banner.keys'), '/keys')}
        >
          {queueData.resets_at === null
            ? t('user.crawl.banner.waiting', { time: '--:--' })
            : splitTime(
                t('user.crawl.banner.waiting', { time: '\u0000' }),
                <Countdown
                  receivedAt={queue.dataUpdatedAt}
                  resetsAt={queueData.resets_at}
                  serverTime={queueData.server_time}
                  stale={stale}
                  onZero={() => {
                    void queue.refetch();
                  }}
                />,
              )}
        </Banner>
      );
    } else if (state === 'key_exhausted') {
      banner = (
        <Banner
          tone="info"
          role="status"
          testId="user-crawl-banner-exhausted"
          action={keysLink('user-crawl-banner-action', t('user.crawl.banner.keys'), '/keys')}
        >
          {t('user.crawl.banner.exhausted', {
            keys: queueData.exhausted_keys.map((key) => key.label).join(', '),
          })}
        </Banner>
      );
    }
  }

  const staleBanner = stale ? (
    <Banner
      tone="warn"
      role="status"
      testId="user-crawl-stale-banner"
      action={
        <button
          type="button"
          className={styles.btn}
          onClick={() => {
            void queue.refetch();
          }}
          data-testid="user-crawl-stale-retry"
        >
          {t('common.retry')}
        </button>
      }
    >
      {t('user.crawl.stale.queue', {
        time: queue.dataUpdatedAt === 0 ? '–' : formatClock(queue.dataUpdatedAt, locale),
      })}
    </Banner>
  ) : null;

  const head = (
    <div className={styles.head}>
      <div>
        <h1 tabIndex={-1} ref={headingRef} data-testid="user-crawl-title">
          {t('user.crawl.title')}
        </h1>
        <p className={styles.lead} data-testid="user-crawl-lead">
          {t('user.crawl.lead')}
        </p>
      </div>
      {settings.data === undefined && queue.data === undefined ? null : (
        <button
          type="button"
          className={styles.btn}
          disabled={pausing || settings.data === undefined}
          onClick={() => {
            void togglePaused();
          }}
          data-testid="user-crawl-pause-all"
        >
          {pausing ? <span className={styles.spin} aria-hidden="true" /> : null}
          {t(paused ? 'user.crawl.resume-all' : 'user.crawl.pause-all')}
        </button>
      )}
    </div>
  );

  const depthCard =
    settings.data !== undefined ? (
      <DepthCard settings={settings.data} onSave={changeDepth} />
    ) : settings.isError ? (
      <section className={styles.card}>
        <SectionError
          name="settings"
          onRetry={() => {
            void settings.refetch();
          }}
        />
      </section>
    ) : (
      <SkeletonBlock />
    );

  const sections = (
    <>
      <div className={styles.g3}>
        {depthCard}
        {empty ? null : queue.isPending ? (
          <SkeletonBlock />
        ) : (
          <LanesCard queue={queueData} unavailable={stale && queueData === undefined} />
        )}
        {empty ? null : summary.data !== undefined ? (
          <RecrawlCard summary={summary.data} />
        ) : summary.isError ? (
          <section className={styles.card}>
            <SectionError
              name="summary"
              onRetry={() => {
                void summary.refetch();
              }}
            />
          </section>
        ) : (
          <SkeletonBlock />
        )}
      </div>

      {empty ? (
        <div className={styles.block} data-testid="user-crawl-empty">
          <span className={styles.blockIcon}>
            <Icon name="activity" size={30} />
          </span>
          <h2>{t('user.crawl.empty.title')}</h2>
          <p>{t('user.crawl.empty.text')}</p>
          <Link
            to={state === 'no_key' ? '/keys?add=1' : '/accounts'}
            className={`${styles.btn} ${styles.primary}`}
            data-testid="user-crawl-empty-cta"
          >
            {t(state === 'no_key' ? 'user.crawl.empty.cta-key' : 'user.crawl.empty.cta-account')}
          </Link>
        </div>
      ) : (
        <>
          {depths.data !== undefined ? (
            <PerDepth
              rows={depths.data}
              currentDepth={settings.data?.depth ?? 1}
              selected={depthFilter}
              highlight={highlight}
              onSelect={setDepthFilter}
            />
          ) : depths.isError ? (
            <SectionError
              name="depths"
              onRetry={() => {
                void depths.refetch();
              }}
            />
          ) : (
            <SkeletonBlock />
          )}

          {interests.data !== undefined ? (
            <Contacts
              items={interestItems}
              filter={filter}
              depthFilter={depthFilter}
              now={now}
              hasMore={interests.hasNextPage}
              loadingMore={interests.isFetchingNextPage}
              busy={busy}
              onFilter={setFilter}
              onClear={clearFilters}
              onLoadMore={() => {
                void interests.fetchNextPage();
              }}
              onStar={(item) => {
                void runContactAction(item, { starred: !item.starred }, (current) => ({
                  ...current,
                  starred: !current.starred,
                }));
              }}
              onPrioritize={(item) => {
                void runContactAction(item, { selected: true }, undefined, () => {
                  toast(t('user.crawl.toast.selected', { name: item.name }));
                });
              }}
              onPause={(item) => {
                void runContactAction(item, { paused: !item.paused });
              }}
            />
          ) : interests.isError ? (
            <SectionError
              name="contacts"
              onRetry={() => {
                void interests.refetch();
              }}
            />
          ) : (
            <SkeletonBlock height={240} />
          )}

          {jobs.data !== undefined ? (
            <Jobs
              items={jobItems}
              hasMore={jobs.hasNextPage}
              loadingMore={jobs.isFetchingNextPage}
              onOpen={openJob}
              onMore={() => {
                void jobs.fetchNextPage();
              }}
            />
          ) : jobs.isError ? (
            <SectionError
              name="jobs"
              onRetry={() => {
                void jobs.refetch();
              }}
            />
          ) : (
            <SkeletonBlock height={160} />
          )}
        </>
      )}
    </>
  );

  let body;
  if (allFailed) {
    body = (
      <div className={styles.block} role="alert" data-testid="user-crawl-error">
        <span className={styles.blockIcon}>
          <Icon name="alert" size={30} />
        </span>
        <h2>{t('user.crawl.error.title')}</h2>
        <p>{t('user.crawl.error.text')}</p>
        <button
          type="button"
          className={`${styles.btn} ${styles.primary}`}
          onClick={retryAll}
          data-testid="user-crawl-error-retry"
        >
          {t('common.retry')}
        </button>
      </div>
    );
  } else if (allPending) {
    body = skeleton ? (
      <div data-testid="user-crawl-skeleton">
        <p className={styles.loadingLine} role="status">
          <span className={styles.spin} aria-hidden="true" />
          {t('user.crawl.loading')}
        </p>
        <div className={styles.g3}>
          <SkeletonBlock />
          <SkeletonBlock />
          <SkeletonBlock />
        </div>
        <div className={styles.skelRow}>
          <SkeletonBlock height={140} />
          <SkeletonBlock height={140} />
          <SkeletonBlock height={140} />
        </div>
        {Array.from({ length: 6 }, (_, index) => (
          <SkeletonBlock key={index} height={34} />
        ))}
      </div>
    ) : null;
  } else {
    body = sections;
  }

  return (
    <div className={styles.page} data-testid="user-crawl-page">
      {head}
      {allFailed || allPending ? null : (
        <>
          {banner}
          {staleBanner}
        </>
      )}
      <div className={styles.body} aria-busy={anyPending}>
        {body}
      </div>
      {jobId === null ? null : (
        <JobDrawer
          id={jobId}
          job={job.data}
          error={job.error}
          retrying={retrying}
          openerRef={opener}
          onClose={closeJob}
          onRetry={() => {
            void retryCurrent();
          }}
          onReload={() => {
            void job.refetch();
          }}
        />
      )}
      <div className={styles.toasts} role="status" aria-live="polite">
        {toasts.map((item) => (
          <div
            key={item.id}
            className={`${styles.toast} ${item.tone === 'bad' ? styles.toastBad : ''}`}
            data-testid="user-crawl-toast"
          >
            {item.text}
          </div>
        ))}
      </div>
      <div
        className={styles.srOnly}
        role="status"
        aria-live="polite"
        data-testid="user-crawl-live-region"
      >
        {announcement}
      </div>
    </div>
  );
}

/** Places `node` where the placeholder (U+0000) of an already translated message stands. */
function splitTime(message: string, node: React.ReactNode): React.ReactNode {
  const [before = '', after = ''] = message.split('\u0000');
  return (
    <>
      {before}
      {node}
      {after}
    </>
  );
}
