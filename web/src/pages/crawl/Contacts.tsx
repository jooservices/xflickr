import { useRef } from 'react';
import { useI18n } from '../../shared';
import type { Interest, InterestFilter } from '../../features/crawl';
import { formatAgo, initials } from './format';
import { Bar, Pill } from './parts';
import type { Tone } from './parts';
import styles from './CrawlPage.module.css';

export const FILTERS: InterestFilter[] = ['all', 'starred', 'selected', 'crawling', 'attention'];

const STATUS_TONE: Record<Interest['status'], Tone> = {
  done: 'ok',
  crawling: 'info',
  queued: 'neutral',
  paused: 'neutral',
  error: 'bad',
};

type ContactsProps = {
  items: Interest[];
  filter: InterestFilter;
  depthFilter: number | undefined;
  now: number;
  hasMore: boolean;
  loadingMore: boolean;
  /** Person ids with a request in flight: their buttons ignore clicks. */
  busy: ReadonlySet<string>;
  onFilter: (filter: InterestFilter) => void;
  onClear: () => void;
  onLoadMore: () => void;
  onStar: (item: Interest) => void;
  onPrioritize: (item: Interest) => void;
  onPause: (item: Interest) => void;
};

export function Contacts({
  items,
  filter,
  depthFilter,
  now,
  hasMore,
  loadingMore,
  busy,
  onFilter,
  onClear,
  onLoadMore,
  onStar,
  onPrioritize,
  onPause,
}: ContactsProps) {
  const { t, locale } = useI18n();
  const number = new Intl.NumberFormat(locale);
  const caption = useRef<HTMLTableCaptionElement>(null);
  const filtered = filter !== 'all' || depthFilter !== undefined;

  return (
    <section
      className={styles.panel}
      aria-labelledby="user-crawl-contacts-title"
      data-testid="user-crawl-contacts"
    >
      <h3 id="user-crawl-contacts-title">{t('user.crawl.contacts.title')}</h3>
      <div className={styles.chips}>
        {FILTERS.map((name) => (
          <button
            key={name}
            type="button"
            className={styles.chip}
            aria-pressed={filter === name}
            onClick={() => {
              onFilter(name);
              caption.current?.focus();
            }}
            data-testid={`user-crawl-filter-${name}`}
          >
            {t(`user.crawl.filter.${name}`)}
          </button>
        ))}
      </div>
      {items.length === 0 && filtered ? (
        <div className={styles.emptyInline}>
          <p>{t('user.crawl.empty.filtered')}</p>
          <button
            type="button"
            className={styles.btn}
            onClick={onClear}
            data-testid="user-crawl-filter-clear"
          >
            {t('user.crawl.empty.clear')}
          </button>
        </div>
      ) : (
        <div className={styles.tableWrap}>
          <table className={styles.table}>
            <caption ref={caption} tabIndex={-1} className={styles.srOnly}>
              {t('user.crawl.contacts.title')}
            </caption>
            <thead>
              <tr>
                <th scope="col">{t('user.crawl.col.contact')}</th>
                <th scope="col">{t('user.crawl.col.depth')}</th>
                <th scope="col">{t('user.crawl.col.status')}</th>
                <th scope="col">{t('user.crawl.col.photos')}</th>
                <th scope="col">{t('user.crawl.col.sizes')}</th>
                <th scope="col">{t('user.crawl.col.checked')}</th>
                <th scope="col">
                  <span className={styles.srOnly}>{t('user.crawl.col.actions')}</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {items.map((item) => {
                const selected =
                  item.selected_until !== null && Date.parse(item.selected_until) > now;
                const pending = busy.has(item.person_id);
                return (
                  <tr key={item.person_id} data-testid={`user-crawl-contact-${item.person_id}`}>
                    <td data-label={t('user.crawl.col.contact')}>
                      <span className={styles.who}>
                        <span className={styles.avatar} aria-hidden="true">
                          {initials(item.name)}
                        </span>
                        <b>{item.name}</b>
                        {selected ? <Pill tone="info">{t('user.crawl.selected')}</Pill> : null}
                      </span>
                    </td>
                    <td className={styles.num} data-label={t('user.crawl.col.depth')}>
                      {item.depth}
                    </td>
                    <td data-label={t('user.crawl.col.status')}>
                      <Pill
                        tone={STATUS_TONE[item.status]}
                        testId={`user-crawl-contact-status-${item.person_id}`}
                      >
                        {t(`user.crawl.status.${item.status}`)}
                      </Pill>
                    </td>
                    <td data-label={t('user.crawl.col.photos')}>
                      <span className={styles.photos}>
                        <Bar
                          label={`${t('user.crawl.col.photos')}: ${item.name}`}
                          value={item.photos_crawled}
                          max={item.photos_total}
                          text={t('user.crawl.depths.done', {
                            done: number.format(item.photos_crawled),
                            total: number.format(item.photos_total),
                          })}
                        />
                        <span className={`${styles.small} ${styles.num}`}>
                          {number.format(item.photos_crawled)}/{number.format(item.photos_total)}
                        </span>
                      </span>
                    </td>
                    <td className={styles.num} data-label={t('user.crawl.col.sizes')}>
                      {number.format(item.sizes_percent)}%
                    </td>
                    <td className={styles.muted} data-label={t('user.crawl.col.checked')}>
                      {formatAgo(item.last_check_at, now, t)}
                    </td>
                    <td>
                      <div className={styles.acts}>
                        <button
                          type="button"
                          className={`${styles.btn} ${styles.sm} ${styles.starBtn}`}
                          aria-pressed={item.starred}
                          aria-label={t(item.starred ? 'user.crawl.unstar' : 'user.crawl.star', {
                            name: item.name,
                          })}
                          disabled={pending}
                          onClick={() => {
                            onStar(item);
                          }}
                          data-testid={`user-crawl-contact-star-${item.person_id}`}
                        >
                          <span aria-hidden="true">{item.starred ? '★' : '☆'}</span>
                        </button>
                        <button
                          type="button"
                          className={`${styles.btn} ${styles.sm}`}
                          aria-label={t('user.crawl.prioritize.aria', { name: item.name })}
                          disabled={pending}
                          onClick={() => {
                            onPrioritize(item);
                          }}
                          data-testid={`user-crawl-contact-prioritize-${item.person_id}`}
                        >
                          {t('user.crawl.prioritize')}
                        </button>
                        <button
                          type="button"
                          className={`${styles.btn} ${styles.sm} ${styles.ghost}`}
                          aria-label={t(
                            item.paused ? 'user.crawl.resume.aria' : 'user.crawl.pause.aria',
                            {
                              name: item.name,
                            },
                          )}
                          disabled={pending}
                          onClick={() => {
                            onPause(item);
                          }}
                          data-testid={`user-crawl-contact-pause-${item.person_id}`}
                        >
                          {t(item.paused ? 'user.crawl.resume' : 'user.crawl.pause')}
                        </button>
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
      {hasMore ? (
        <div className={styles.more}>
          <button
            type="button"
            className={styles.btn}
            disabled={loadingMore}
            onClick={onLoadMore}
            data-testid="user-crawl-load-more"
          >
            {loadingMore ? <span className={styles.spin} aria-hidden="true" /> : null}
            {t('user.crawl.load-more')}
          </button>
        </div>
      ) : null}
    </section>
  );
}
