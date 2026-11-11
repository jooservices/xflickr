import { useEffect, useRef } from 'react';
import type { JSX } from 'react';
import { Link } from 'react-router';
import { CrawlApiError, useSettingsQuery } from '../../features/crawl';
import { Icon, useI18n } from '../../shared';
import styles from '../crawl/CrawlPage.module.css';

const status = (error: unknown): number | undefined =>
  error instanceof CrawlApiError ? error.status : undefined;

/** Shows the signed-in user's effective crawl depth while contacts are unavailable. */
export function ContactsPage(): JSX.Element {
  const { t } = useI18n();
  const settings = useSettingsQuery();
  const headingRef = useRef<HTMLHeadingElement>(null);
  const forbidden = status(settings.error) === 403;

  useEffect(() => {
    document.title = `${t('shell.contacts.title')} · XFlickr`;
    headingRef.current?.focus();
  }, [t]);

  let content: JSX.Element;
  if (forbidden) {
    content = (
      <section className={styles.block} role="alert" data-testid="user-contacts-forbidden">
        <span className={styles.blockIcon}>
          <Icon name="lock" size={36} />
        </span>
        <h2>{t('shell.contacts.forbidden')}</h2>
        <Link to="/" className={`${styles.btn} ${styles.primary}`}>
          {t('shell.contacts.back')}
        </Link>
      </section>
    );
  } else if (settings.isError) {
    content = (
      <section className={styles.block} role="alert" data-testid="user-contacts-error">
        <span className={styles.blockIcon}>
          <Icon name="alert" size={36} />
        </span>
        <h2>{t('shell.contacts.error.title')}</h2>
        <p>{t('shell.contacts.error.text')}</p>
        <button
          type="button"
          className={`${styles.btn} ${styles.primary}`}
          onClick={() => void settings.refetch()}
          data-testid="user-contacts-retry"
        >
          {t('shell.contacts.retry')}
        </button>
      </section>
    );
  } else if (settings.isPending || settings.data === undefined) {
    content = (
      <p
        className={styles.loadingLine}
        role="status"
        aria-label={t('shell.contacts.loading')}
        data-testid="user-contacts-loading"
      >
        <span className={styles.spin} aria-hidden="true" />
        {t('shell.contacts.loading')}
      </p>
    );
  } else {
    content = (
      <section className={styles.block} data-testid="user-contacts-unavailable">
        <span className={styles.blockIcon}>
          <Icon name="info" size={36} />
        </span>
        <p>{t('shell.contacts.unavailable')}</p>
      </section>
    );
  }

  return (
    <div className={styles.page} data-testid="user-contacts-page">
      <header className={styles.head}>
        <div>
          <h1 tabIndex={-1} ref={headingRef} data-testid="user-contacts-title">
            {t('shell.contacts.title')}
          </h1>
          {!forbidden && !settings.isError && settings.data !== undefined ? (
            <p className={styles.lead} data-testid="user-contacts-description">
              {t('shell.contacts.description', { depth: settings.data.depth })}
            </p>
          ) : null}
        </div>
      </header>
      <div className={styles.body} aria-busy={settings.isPending}>
        {content}
      </div>
    </div>
  );
}
