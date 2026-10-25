import { useEffect, useRef } from 'react';
import type { RefObject } from 'react';
import { Icon, catalogs, useI18n } from '../../shared';
import type { MessageKey } from '../../shared';
import { CrawlApiError } from '../../features/crawl';
import type { JobDetail } from '../../features/crawl';
import { DASH, facetLabel, formatCountdown, formatDateTime, useNow } from './format';
import { Pill, SkeletonBlock } from './parts';
import { JOB_TONE } from './Jobs';
import styles from './CrawlPage.module.css';

const FOCUSABLE =
  'a[href], button:not([disabled]), input:not([disabled]), [tabindex]:not([tabindex="-1"])';

type JobDrawerProps = {
  id: string;
  job: JobDetail | undefined;
  error: unknown;
  retrying: boolean;
  /** The row that opened the drawer; focus returns to it on close. */
  openerRef: RefObject<HTMLElement | null>;
  onClose: () => void;
  onRetry: () => void;
  onReload: () => void;
};

/**
 * Right-hand drawer (bottom sheet on mobile): focus moves in on open and is trapped, Esc
 * closes, the page behind does not scroll, and focus returns to the opening row.
 */
export function JobDrawer({
  id,
  job,
  error,
  retrying,
  openerRef,
  onClose,
  onRetry,
  onReload,
}: JobDrawerProps) {
  const { t, locale } = useI18n();
  const now = useNow();
  const ref = useRef<HTMLElement>(null);
  const closeRef = useRef(onClose);

  useEffect(() => {
    closeRef.current = onClose;
  }, [onClose]);

  useEffect(() => {
    const opener =
      openerRef.current ??
      (document.activeElement instanceof HTMLElement ? document.activeElement : null);
    const container = ref.current;
    container?.querySelector<HTMLElement>(FOCUSABLE)?.focus();
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';

    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.stopPropagation();
        closeRef.current();
        return;
      }
      if (event.key !== 'Tab' || container === null) return;
      const items = Array.from(container.querySelectorAll<HTMLElement>(FOCUSABLE));
      const head = items[0];
      const tail = items[items.length - 1];
      if (head === undefined || tail === undefined) return;
      if (event.shiftKey && document.activeElement === head) {
        event.preventDefault();
        tail.focus();
      } else if (!event.shiftKey && document.activeElement === tail) {
        event.preventDefault();
        head.focus();
      }
    };
    document.addEventListener('keydown', onKeyDown);
    return () => {
      document.removeEventListener('keydown', onKeyDown);
      document.body.style.overflow = previousOverflow;
      if (opener?.isConnected === true) opener.focus();
    };
  }, [openerRef]);

  const gone = error instanceof CrawlApiError && error.status === 404;
  const retryable = job !== undefined && (job.state === 'retry' || job.state === 'failed');
  const retryIn = job?.next_retry_at == null ? 0 : Date.parse(job.next_retry_at) - now;
  const errorCode = job?.error?.code ?? '';
  const codeKey = `user.crawl.error.code.${errorCode}`;

  return (
    <>
      <div className={styles.scrim} onClick={onClose} aria-hidden="true" />
      <aside
        ref={ref}
        className={styles.drawer}
        role="dialog"
        aria-modal="true"
        aria-labelledby="user-crawl-drawer-title"
        data-testid="user-crawl-job-drawer"
      >
        <div className={styles.drawerHead}>
          <h2 id="user-crawl-drawer-title">{t('user.crawl.job.title')}</h2>
          <span className={`${styles.mono} ${styles.small}`} data-testid="user-crawl-job-drawer-id">
            {id}
          </span>
          {job === undefined ? null : (
            <Pill tone={JOB_TONE[job.state]} testId="user-crawl-job-drawer-state">
              {t(`user.crawl.job.state.${job.state}`)}
            </Pill>
          )}
          <span className={styles.grow} />
          <button
            type="button"
            className={styles.iconBtn}
            aria-label={t('user.crawl.job.close')}
            onClick={onClose}
            data-testid="user-crawl-job-drawer-close"
          >
            <Icon name="x" />
          </button>
        </div>
        <div className={styles.drawerBody}>
          {gone ? (
            <p data-testid="user-crawl-job-drawer-gone">{t('user.crawl.job.gone')}</p>
          ) : error !== undefined && error !== null && job === undefined ? (
            <div className={styles.sectionError} role="alert">
              <Icon name="alert" size={16} />
              <span>{t('user.crawl.error.job')}</span>
              <button
                type="button"
                className={`${styles.btn} ${styles.sm}`}
                onClick={onReload}
                data-testid="user-crawl-job-drawer-reload"
              >
                {t('common.retry')}
              </button>
            </div>
          ) : job === undefined ? (
            <SkeletonBlock height={200} />
          ) : (
            <>
              <dl className={styles.kv}>
                <dt>{t('user.crawl.job.id')}</dt>
                <dd className={styles.mono}>{job.id}</dd>
                <dt>{t('user.crawl.job.contact')}</dt>
                <dd data-testid="user-crawl-job-drawer-contact">{job.contact_name}</dd>
                <dt>{t('user.crawl.job.facet')}</dt>
                <dd data-testid="user-crawl-job-drawer-facet">{facetLabel(t, job.facet)}</dd>
                <dt>{t('user.crawl.job.lane')}</dt>
                <dd>{t(`user.crawl.lane.${job.lane}`)}</dd>
                <dt>{t('user.crawl.job.page')}</dt>
                <dd data-testid="user-crawl-job-drawer-page">
                  {job.page === null || job.pages === null
                    ? DASH
                    : t('user.crawl.job.page', { page: job.page, pages: job.pages })}
                </dd>
                <dt>{t('user.crawl.job.state')}</dt>
                <dd>{t(`user.crawl.job.state.${job.state}`)}</dd>
                <dt>{t('user.crawl.col.attempt')}</dt>
                <dd data-testid="user-crawl-job-drawer-attempt">
                  {t('user.crawl.job.attempt', { n: job.attempt, max: job.max_attempts })}
                </dd>
                <dt>{t('user.crawl.job.retries')}</dt>
                <dd>{Math.max(0, job.attempt - 1)}</dd>
                <dt>{t('user.crawl.job.key')}</dt>
                <dd>{job.key_label ?? DASH}</dd>
                <dt>{t('user.crawl.job.priority')}</dt>
                <dd>{t(`user.crawl.priority.${job.priority}`)}</dd>
                <dt>{t('user.crawl.job.calls')}</dt>
                <dd className={styles.num}>{new Intl.NumberFormat(locale).format(job.calls)}</dd>
                <dt>{t('user.crawl.job.records')}</dt>
                <dd className={styles.num}>{new Intl.NumberFormat(locale).format(job.records)}</dd>
              </dl>
              <dl className={styles.kv} data-testid="user-crawl-job-drawer-times">
                <dt>{t('user.crawl.job.created')}</dt>
                <dd>{formatDateTime(job.created_at, locale)}</dd>
                <dt>{t('user.crawl.job.started')}</dt>
                <dd>{formatDateTime(job.started_at, locale)}</dd>
                <dt>{t('user.crawl.job.finished')}</dt>
                <dd>{formatDateTime(job.finished_at, locale)}</dd>
              </dl>
              {job.next_retry_at !== null && retryIn > 0 ? (
                <p className={styles.nextRetry} data-testid="user-crawl-job-drawer-next-retry">
                  {t('user.crawl.job.next-retry', { time: formatCountdown(retryIn) })}
                </p>
              ) : null}
              {job.error === null ? null : (
                <div className={styles.errorRow} data-testid="user-crawl-job-drawer-error">
                  <b>{t('user.crawl.job.error')}</b>
                  <p>
                    {codeKey in catalogs.en
                      ? t(codeKey as MessageKey)
                      : t('user.crawl.error.code.unknown')}
                  </p>
                  <pre className={styles.mono}>{job.error.message}</pre>
                </div>
              )}
              <h3 className={styles.drawerH3}>{t('user.crawl.job.attempts')}</h3>
              {job.attempts.length === 0 ? (
                <p className={`${styles.small} ${styles.muted}`}>
                  {t('user.crawl.job.attempts-empty')}
                </p>
              ) : (
                <table className={styles.table} data-testid="user-crawl-job-drawer-attempts">
                  <thead>
                    <tr>
                      <th scope="col">{t('user.crawl.attempt.n')}</th>
                      <th scope="col">{t('user.crawl.attempt.at')}</th>
                      <th scope="col">{t('user.crawl.attempt.result')}</th>
                      <th scope="col">{t('user.crawl.attempt.duration')}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {job.attempts.map((attempt) => {
                      const resultKey = `user.crawl.attempt.${attempt.result}`;
                      return (
                        <tr key={attempt.n}>
                          <td className={styles.num}>{attempt.n}</td>
                          <td>{formatDateTime(attempt.at, locale)}</td>
                          <td>
                            {resultKey in catalogs.en ? t(resultKey as MessageKey) : attempt.result}
                          </td>
                          <td className={styles.num}>
                            {(attempt.duration_ms / 1000).toFixed(1)} s
                          </td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              )}
            </>
          )}
        </div>
        {retryable ? (
          <div className={styles.drawerFoot}>
            <button
              type="button"
              className={`${styles.btn} ${styles.primary}`}
              disabled={retrying}
              onClick={onRetry}
              data-testid="user-crawl-job-drawer-retry"
            >
              {retrying ? <span className={styles.spin} aria-hidden="true" /> : null}
              {t('user.crawl.job.retry')}
            </button>
          </div>
        ) : null}
      </aside>
    </>
  );
}
