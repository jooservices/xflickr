import { useQueryClient } from '@tanstack/react-query';
import { useCallback, useEffect, useRef, useState } from 'react';
import { Link } from 'react-router';
import { Icon, useI18n, useLoadingTiming } from '../../shared';
import {
  CrawlApiError,
  crawlKeys,
  saveDepth,
  useQueueQuery,
  useSettingsQuery,
  useStatusQuery,
} from '../../features/crawl';
import { DepthCard, LanesCard, PerDepth } from './Cards';
import { formatClock, useNow } from './format';
import { Banner, SectionError, SkeletonBlock } from './parts';
import styles from './CrawlPage.module.css';

const STALE_AFTER_MS = 30_000;

type Toast = { id: number; text: string; tone: 'ok' | 'bad' };

const status = (error: unknown): number | undefined =>
  error instanceof CrawlApiError ? error.status : undefined;

/** Crawl progress and depth. Contacts and jobs have no endpoint yet and show as not available. */
export function CrawlPage() {
  const { t, locale } = useI18n();
  const client = useQueryClient();
  const now = useNow(true, 5000);

  const queue = useQueueQuery();
  const settings = useSettingsQuery();
  const progress = useStatusQuery();

  const [toasts, setToasts] = useState<Toast[]>([]);
  const nextToast = useRef(1);
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

  const core = [queue, settings, progress];
  const hasData = core.some((query) => query.data !== undefined);
  const forbidden = !hasData && core.some((query) => status(query.error) === 403);
  const allFailed = core.every((query) => query.isError);
  const allPending = core.every((query) => query.isPending);
  const anyPending = core.some((query) => query.isPending);
  const skeleton = useLoadingTiming(anyPending, 150);

  const retryAll = () => {
    for (const query of core) if (query.isError) void query.refetch();
  };

  const changeDepth = async (depth: number): Promise<boolean> => {
    try {
      const saved = await saveDepth(depth);
      client.setQueryData(crawlKeys.settings, saved);
      toast(t('user.crawl.toast.depth', { n: saved.depth }));
      void client.invalidateQueries({ queryKey: crawlKeys.status });
      return true;
    } catch (error) {
      if (
        error instanceof CrawlApiError &&
        error.status === 422 &&
        error.code === 'depth_above_cap'
      ) {
        const max = Number(error.details?.max_depth ?? settings.data?.depth_max ?? depth);
        toast(t('user.crawl.error.depth-cap', { max }), 'bad');
      } else {
        toast(t('user.crawl.error.action-failed'), 'bad');
      }
      return false;
    }
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
          <Link
            to="/"
            className={`${styles.btn} ${styles.primary}`}
            data-testid="user-crawl-forbidden-cta"
          >
            {t('user.crawl.forbidden.cta')}
          </Link>
        </div>
      </div>
    );
  }

  const queueData = queue.data;
  const age = queue.dataUpdatedAt === 0 ? 0 : now - queue.dataUpdatedAt;
  const stale = queue.isError || (queueData !== undefined && age > STALE_AFTER_MS);

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
      <div className={styles.g2}>
        {depthCard}
        {queue.isPending ? (
          <SkeletonBlock />
        ) : (
          <LanesCard queue={queueData} unavailable={stale && queueData === undefined} />
        )}
      </div>

      {progress.data !== undefined ? (
        <PerDepth rows={progress.data.depths} currentDepth={settings.data?.depth ?? 1} />
      ) : progress.isError ? (
        <SectionError
          name="depths"
          onRetry={() => {
            void progress.refetch();
          }}
        />
      ) : (
        <SkeletonBlock />
      )}

      <div className={styles.block} data-testid="user-crawl-unavailable">
        <span className={styles.blockIcon}>
          <Icon name="info" size={30} />
        </span>
        <h2>{t('common.not-available.title')}</h2>
        <p>{t('user.crawl.unavailable.text')}</p>
      </div>
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
        <div className={styles.g2}>
          <SkeletonBlock />
          <SkeletonBlock />
        </div>
        <SkeletonBlock height={140} />
      </div>
    ) : null;
  } else {
    body = sections;
  }

  return (
    <div className={styles.page} data-testid="user-crawl-page">
      <div className={styles.head}>
        <div>
          <h1 tabIndex={-1} ref={headingRef} data-testid="user-crawl-title">
            {t('user.crawl.title')}
          </h1>
          <p className={styles.lead} data-testid="user-crawl-lead">
            {t('user.crawl.lead')}
          </p>
        </div>
      </div>
      {allFailed || allPending ? null : staleBanner}
      <div className={styles.body} aria-busy={anyPending}>
        {body}
      </div>
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
    </div>
  );
}
