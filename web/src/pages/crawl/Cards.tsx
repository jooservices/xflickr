import { useState } from 'react';
import { useI18n } from '../../shared';
import type { CrawlQueue, CrawlSettings, CrawlSummary, DepthRow, Lane } from '../../features/crawl';
import { DASH, formatEta } from './format';
import { Bar, Pill } from './parts';
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
  const value = Math.min(draft ?? settings.depth, settings.max_depth);
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
        max={Math.max(1, settings.max_depth)}
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
        <span>{settings.max_depth}</span>
      </div>
      <p
        id="user-crawl-depth-cap"
        className={`${styles.small} ${styles.muted}`}
        data-testid="user-crawl-depth-cap"
      >
        {t('user.crawl.depth.cap', { default: settings.default_depth, max: settings.max_depth })}
      </p>
      {value > settings.default_depth + 2 ? (
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
        const state = queue?.state ?? 'running';
        const noKey = state === 'no_key';
        const showNumbers = !blank && row !== undefined && !noKey;
        const backlog = row?.backlog_calls ?? 0;
        const done = row?.done_calls ?? 0;
        let eta = DASH;
        if (!blank && row !== undefined) {
          if (state === 'waiting_quota') eta = t('user.crawl.lane.reason.waiting_quota');
          else if (state !== 'paused' && !noKey)
            eta = formatEta(backlog === 0 ? null : row.eta_hours, t);
        }
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
            <Bar
              label={t(`user.crawl.lane.${lane}`)}
              value={showNumbers ? done : 0}
              max={showNumbers ? done + backlog : 0}
              text={t('user.crawl.depths.done', {
                done: number.format(showNumbers ? done : 0),
                total: number.format(showNumbers ? done + backlog : 0),
              })}
            />
            <div className={`${styles.row} ${styles.between} ${styles.small}`}>
              <span className={styles.num} data-testid={`user-crawl-lane-position-${lane}`}>
                {showNumbers && row.position != null
                  ? t('user.crawl.lane.position', { n: row.position })
                  : DASH}
              </span>
              <span className={styles.num} data-testid={`user-crawl-lane-eta-${lane}`}>
                {eta === DASH || state === 'waiting_quota'
                  ? eta
                  : t('user.crawl.lane.eta', { time: eta })}
              </span>
            </div>
          </div>
        );
      })}
      <p className={`${styles.small} ${styles.muted}`}>{t('user.crawl.lanes.hint')}</p>
    </section>
  );
}

export function RecrawlCard({ summary }: { summary: CrawlSummary }) {
  const { t, locale } = useI18n();
  const number = new Intl.NumberFormat(locale);

  return (
    <section
      className={styles.card}
      aria-labelledby="user-crawl-recrawl-title"
      data-testid="user-crawl-recrawl-card"
    >
      <h3 id="user-crawl-recrawl-title">{t('user.crawl.recrawl.title')}</h3>
      <p className={`${styles.small} ${styles.muted}`}>
        {t('user.crawl.recrawl.text', { hours: summary.recrawl_interval_hours })}
      </p>
      <p className={styles.small}>
        {t('user.crawl.recrawl.cycle', {
          done: number.format(summary.contacts_done),
          total: number.format(summary.contacts_total),
        })}
      </p>
      <p className={styles.small}>
        {t('user.crawl.recrawl.saved', { count: number.format(summary.calls_saved_week) })}
      </p>
    </section>
  );
}

type PerDepthProps = {
  rows: DepthRow[];
  currentDepth: number;
  selected: number | undefined;
  highlight: number | undefined;
  onSelect: (depth: number | undefined) => void;
};

export function PerDepth({ rows, currentDepth, selected, highlight, onSelect }: PerDepthProps) {
  const { t, locale } = useI18n();
  const number = new Intl.NumberFormat(locale);
  const byDepth = new Map(rows.map((row) => [row.depth, row]));
  const last = Math.max(currentDepth, ...rows.map((row) => row.depth));
  const columns: DepthRow[] = Array.from({ length: last + 1 }, (_, depth) => {
    return (
      byDepth.get(depth) ?? {
        depth,
        contacts_total: 0,
        done: 0,
        crawling: 0,
        waiting: 0,
        error: 0,
        beyond_setting: depth > currentDepth,
      }
    );
  });

  return (
    <section
      className={styles.panel}
      aria-labelledby="user-crawl-depths-title"
      data-testid="user-crawl-depths"
    >
      <h3 id="user-crawl-depths-title">{t('user.crawl.depths.title')}</h3>
      <div className={styles.depths}>
        {columns.map((column) => {
          const total = column.contacts_total;
          const pct = (n: number) => `${String(total === 0 ? 0 : (n / total) * 100)}%`;
          const title =
            column.depth === 0
              ? t('user.crawl.depths.own')
              : t('user.crawl.depths.column', { n: column.depth });
          const doneText = t('user.crawl.depths.done', {
            done: number.format(column.done),
            total: number.format(total),
          });
          const beyond = column.beyond_setting && total > 0;
          const toggle = () => {
            onSelect(selected === column.depth ? undefined : column.depth);
          };
          return (
            <div
              key={column.depth}
              className={`${styles.dcol} ${beyond ? styles.beyond : ''} ${highlight === column.depth ? styles.highlight : ''}`}
              onClick={toggle}
            >
              <button
                type="button"
                className={styles.dcolHead}
                aria-pressed={selected === column.depth}
                data-testid={`user-crawl-depth-column-${String(column.depth)}`}
              >
                <span className={styles.row}>
                  <b>{title}</b>
                  {beyond ? <Pill tone="neutral">{t('user.crawl.depth.beyond')}</Pill> : null}
                </span>
                <span className={`${styles.big} ${styles.num}`}>{number.format(total)}</span>
                <span className={`${styles.small} ${styles.muted}`}>
                  {t('user.crawl.depths.count', { count: number.format(total) })}
                </span>
              </button>
              <span
                className={`${styles.bar} ${styles.stacked}`}
                role="progressbar"
                aria-label={title}
                aria-valuemin={0}
                aria-valuenow={column.done}
                aria-valuemax={total}
                aria-valuetext={doneText}
              >
                <i className={styles.segDone} style={{ width: pct(column.done) }} />
                <i className={styles.segCrawling} style={{ width: pct(column.crawling) }} />
                <i className={styles.segWaiting} style={{ width: pct(column.waiting) }} />
                <i className={styles.segError} style={{ width: pct(column.error) }} />
              </span>
              <span className={`${styles.small} ${styles.muted}`}>
                {t('user.crawl.depths.legend-counts', {
                  done: number.format(column.done),
                  crawling: number.format(column.crawling),
                  waiting: number.format(column.waiting),
                  error: number.format(column.error),
                })}
              </span>
              <span className={styles.small}>{doneText}</span>
            </div>
          );
        })}
      </div>
    </section>
  );
}
