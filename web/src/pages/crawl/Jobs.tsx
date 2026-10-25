import type { MouseEvent } from 'react';
import { useI18n } from '../../shared';
import type { Job, JobState } from '../../features/crawl';
import { facetLabel } from './format';
import { Pill } from './parts';
import type { Tone } from './parts';
import styles from './CrawlPage.module.css';

export const JOB_TONE: Record<JobState, Tone> = {
  pending: 'neutral',
  running: 'info',
  waiting: 'warn',
  retry: 'warn',
  failed: 'bad',
  done: 'ok',
};

type JobsProps = {
  items: Job[];
  hasMore: boolean;
  loadingMore: boolean;
  onOpen: (id: string, opener: HTMLElement) => void;
  onMore: () => void;
};

export function Jobs({ items, hasMore, loadingMore, onOpen, onMore }: JobsProps) {
  const { t } = useI18n();

  const open = (event: MouseEvent<HTMLElement>, id: string) => {
    onOpen(id, event.currentTarget);
  };

  return (
    <section
      className={styles.panel}
      aria-labelledby="user-crawl-jobs-title"
      data-testid="user-crawl-jobs"
    >
      <h3 id="user-crawl-jobs-title">{t('user.crawl.jobs.title')}</h3>
      {items.length === 0 ? (
        <p className={`${styles.emptyInline} ${styles.muted}`}>{t('user.crawl.jobs.empty')}</p>
      ) : (
        <div className={styles.tableWrap}>
          <table className={`${styles.table} ${styles.jobsTable}`}>
            <caption className={styles.srOnly}>{t('user.crawl.jobs.title')}</caption>
            <thead>
              <tr>
                <th scope="col">{t('user.crawl.job.id')}</th>
                <th scope="col">{t('user.crawl.col.contact')}</th>
                <th scope="col">{t('user.crawl.job.facet')}</th>
                <th scope="col">{t('user.crawl.job.state')}</th>
                <th scope="col">{t('user.crawl.col.attempt')}</th>
              </tr>
            </thead>
            <tbody>
              {items.map((job) => (
                <tr
                  key={job.id}
                  className={styles.clickable}
                  onClick={(event) => {
                    open(event, job.id);
                  }}
                  data-testid={`user-crawl-job-${job.id}`}
                >
                  <td>
                    <button
                      type="button"
                      className={styles.linkBtn}
                      onClick={(event) => {
                        event.stopPropagation();
                        open(event, job.id);
                      }}
                    >
                      {job.id}
                    </button>
                  </td>
                  <td>{job.contact_name}</td>
                  <td>{facetLabel(t, job.facet)}</td>
                  <td>
                    <Pill tone={JOB_TONE[job.state]}>{t(`user.crawl.job.state.${job.state}`)}</Pill>
                  </td>
                  <td className={styles.num}>
                    {t('user.crawl.job.attempt', { n: job.attempt, max: job.max_attempts })}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {hasMore ? (
        <div className={styles.more}>
          <button
            type="button"
            className={styles.linkBtn}
            disabled={loadingMore}
            onClick={onMore}
            data-testid="user-crawl-jobs-more"
          >
            {t('user.crawl.jobs.all')}
          </button>
        </div>
      ) : null}
    </section>
  );
}
