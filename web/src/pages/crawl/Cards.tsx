import { useState } from 'react';
import { useI18n } from '../../shared';
import type { CrawlQueue, CrawlSettings, CrawlStatus, Lane } from '../../features/crawl';
import { DASH, formatEta } from './format';
import { Pill } from './parts';
import styles from './CrawlPage.module.css';

type DepthCardProps = {
  settings: CrawlSettings;
  /** Resolves true when the server stored the depth. */
  onSave: (depth: number) => Promise<boolean>;
};

export function DepthCard({ settings, onSave }: DepthCardProps) {
  const { t } = useI18n();
  const [draft, setDraft] = useState<number | null>(null);
  const [sending, setSending] = useState(false);
  const value = Math.min(draft ?? settings.depth, settings.depth_max);
  const changed = value !== settings.depth;

  const save = async () => {
    setSending(true);
    await onSave(value);
    setDraft(null);
    setSending(false);
  };

  return (
    <section
      className={styles.card}
      aria-labelledby="user-crawl-depth-title"
      data-testid="user-crawl-depth-card"
    >
      <h3 id="user-crawl-depth-title">{t('user.crawl.depth.title')}</h3>
      <p className={`${styles.small} ${styles.muted}`}>{t('user.crawl.depth.help')}</p>
      <input
        type="range"
        className={styles.range}
        min={1}
        max={Math.max(1, settings.depth_max)}
        step={1}
        value={value}
        aria-labelledby="user-crawl-depth-title"
        aria-valuetext={t('user.crawl.depth.value', { n: value })}
        aria-describedby="user-crawl-depth-cap"
        onChange={(event) => {
          setDraft(Number(event.target.value));
        }}
        data-testid="user-crawl-depth-slider"
      />
      <div className={`${styles.row} ${styles.between} ${styles.small}`}>
        <span>{t('user.crawl.depth.min')}</span>
        <b data-testid="user-crawl-depth-value">{t('user.crawl.depth.value', { n: value })}</b>
        <span>{settings.depth_max}</span>
      </div>
      <p
        id="user-crawl-depth-cap"
        className={`${styles.small} ${styles.muted}`}
        data-testid="user-crawl-depth-cap"
      >
        {t('user.crawl.depth.cap', { default: settings.depth_default, max: settings.depth_max })}
      </p>
      {value > settings.depth_default + 2 ? (
        <p className={`${styles.small} ${styles.warnText}`}>{t('user.crawl.depth.warn')}</p>
      ) : null}
      {changed ? (
        <button
          type="button"
          className={`${styles.btn} ${styles.primary}`}
          disabled={sending}
          onClick={() => {
            void save();
          }}
          data-testid="user-crawl-depth-save"
        >
          {sending ? <span className={styles.spin} aria-hidden="true" /> : null}
          {t('user.crawl.depth.save')}
        </button>
      ) : null}
    </section>
  );
}

type LanesCardProps = {
  queue: CrawlQueue | undefined;
  /** Queue numbers cannot be trusted: show dashes. */
  unavailable: boolean;
};

const LANES: Lane[] = ['person', 'sizes'];

export function LanesCard({ queue, unavailable }: LanesCardProps) {
  const { t, locale } = useI18n();
  const number = new Intl.NumberFormat(locale);
  const blank = unavailable || queue === undefined;

  return (
    <section
      className={styles.card}
      aria-labelledby="user-crawl-lanes-title"
      data-testid="user-crawl-lanes-card"
    >
      <h3 id="user-crawl-lanes-title">{t('user.crawl.lanes.title')}</h3>
      {LANES.map((lane) => {
        const row = queue?.lanes.find((item) => item.lane === lane);
        const showNumbers = !blank && row !== undefined;
        const backlog = row?.backlog_calls ?? 0;
        const eta = showNumbers ? formatEta(backlog === 0 ? null : row.eta_hours, t) : DASH;
        return (
          <div key={lane} className={styles.lane} data-testid={`user-crawl-lane-${lane}`}>
            <div className={`${styles.row} ${styles.between}`}>
              <b>{t(`user.crawl.lane.${lane}`)}</b>
              <span
                className={`${styles.small} ${styles.num}`}
                data-testid={`user-crawl-lane-backlog-${lane}`}
              >
                {showNumbers
                  ? t('user.crawl.lane.backlog', { count: number.format(backlog) })
                  : DASH}
              </span>
            </div>
            <div className={`${styles.row} ${styles.between} ${styles.small}`}>
              <span className={styles.num} data-testid={`user-crawl-lane-eta-${lane}`}>
                {eta === DASH ? eta : t('user.crawl.lane.eta', { time: eta })}
              </span>
            </div>
          </div>
        );
      })}
      <p className={`${styles.small} ${styles.muted}`}>{t('user.crawl.lanes.hint')}</p>
    </section>
  );
}

type PerDepthProps = {
  rows: CrawlStatus['depths'];
  currentDepth: number;
};

/** Contacts found at each depth; columns past the chosen depth are marked. */
export function PerDepth({ rows, currentDepth }: PerDepthProps) {
  const { t, locale } = useI18n();
  const number = new Intl.NumberFormat(locale);
  const byDepth = new Map(rows.map((row) => [row.depth, row]));
  const last = Math.max(currentDepth, ...rows.map((row) => row.depth));
  const columns = Array.from({ length: last + 1 }, (_, depth) => ({
    depth,
    total: byDepth.get(depth)?.contacts ?? 0,
    beyond: depth > currentDepth,
  }));

  return (
    <section
      className={styles.panel}
      aria-labelledby="user-crawl-depths-title"
      data-testid="user-crawl-depths"
    >
      <h3 id="user-crawl-depths-title">{t('user.crawl.depths.title')}</h3>
      <div className={styles.depths}>
        {columns.map((column) => (
          <div
            key={column.depth}
            className={`${styles.dcol} ${column.beyond && column.total > 0 ? styles.beyond : ''}`}
            data-testid={`user-crawl-depth-column-${String(column.depth)}`}
          >
            <span className={styles.row}>
              <b>
                {column.depth === 0
                  ? t('user.crawl.depths.own')
                  : t('user.crawl.depths.column', { n: column.depth })}
              </b>
              {column.beyond && column.total > 0 ? (
                <Pill tone="neutral">{t('user.crawl.depth.beyond')}</Pill>
              ) : null}
            </span>
            <span className={`${styles.big} ${styles.num}`}>{number.format(column.total)}</span>
            <span className={`${styles.small} ${styles.muted}`}>
              {t('user.crawl.depths.count', { count: number.format(column.total) })}
            </span>
          </div>
        ))}
      </div>
    </section>
  );
}
