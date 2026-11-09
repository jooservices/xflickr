import { useQuery } from '@tanstack/react-query';
import { useEffect } from 'react';
import { Link, useParams, useSearchParams } from 'react-router';
import { getPerson } from '../../shared/api/gen/catalog/catalog';
import type { CatalogPerson } from '../../shared/api/gen/models';
import { useI18n } from '../../shared';
import type { MessageKey } from '../../shared';
import { useStatusQuery } from '../../features/crawl';
import styles from './ContactPage.module.css';

type ApiError = Error & { status: number };
type Tab = 'photos' | 'favorites' | 'albums' | 'galleries' | 'groups' | 'about';

const tabs: Tab[] = ['photos', 'favorites', 'albums', 'galleries', 'groups', 'about'];

function apiError(status: number): ApiError {
  return Object.assign(new Error(`Contact request failed with ${String(status)}`), { status });
}

function errorStatus(error: unknown): number | undefined {
  return typeof error === 'object' && error !== null && 'status' in error
    ? Number(error.status)
    : undefined;
}

function facetName(facet: string, t: (key: MessageKey) => string): string {
  switch (facet) {
    case 'photos':
      return t('contact.facets.photos');
    case 'favorites':
      return t('contact.facets.favorites');
    case 'albums':
      return t('contact.facets.albums');
    case 'galleries':
      return t('contact.facets.galleries');
    case 'groups':
      return t('contact.facets.groups');
    default:
      return facet;
  }
}

async function loadPerson(nsid: string, signal: AbortSignal): Promise<CatalogPerson> {
  const response = await getPerson(nsid, { signal });
  if (response.status !== 200) throw apiError(response.status);
  return response.data;
}

export function ContactPage() {
  const { id = '' } = useParams();
  const { t, locale } = useI18n();
  const [searchParams] = useSearchParams();
  const selectedTab = tabs.find((tab) => tab === searchParams.get('tab')) ?? 'photos';
  const person = useQuery({
    queryKey: ['contacts', 'person', id],
    queryFn: ({ signal }) => loadPerson(id, signal),
    retry: false,
    enabled: id.length > 0,
  });
  const crawl = useStatusQuery();
  useEffect(() => {
    const name = person.data?.realname || person.data?.username || t('contact.title');
    document.title = `${name} · XFlickr`;
  }, [id, person.data, t]);

  if (person.isPending) {
    return (
      <div className={styles.page} aria-busy="true" data-testid="user-contact-loading">
        <p role="status">{t('contact.loading')}</p>
        <div className={styles.skeleton} aria-hidden="true" />
      </div>
    );
  }

  if (person.isError) {
    const status = errorStatus(person.error);
    const forbidden = status === 403;
    const notFound = status === 404;
    return (
      <div
        className={styles.page}
        data-testid={
          forbidden
            ? 'user-contact-forbidden'
            : notFound
              ? 'user-contact-not-found'
              : 'user-contact-error'
        }
      >
        <h1 tabIndex={-1} data-testid="user-contact-title">
          {forbidden
            ? t('contact.forbidden.title')
            : notFound
              ? t('contact.not-found.title')
              : t('contact.error.title')}
        </h1>
        <p>
          {forbidden
            ? t('contact.forbidden.text')
            : notFound
              ? t('contact.not-found.text')
              : t('contact.error.text')}
        </p>
        {status !== 403 && status !== 404 && (
          <button
            className={styles.button}
            onClick={() => void person.refetch()}
            data-testid="user-contact-retry"
          >
            {t('contact.error.retry')}
          </button>
        )}
        <Link className={styles.button} to="/contacts">
          {t('contact.back')}
        </Link>
      </div>
    );
  }

  const name = person.data.realname || person.data.username || person.data.nsid;
  const selected = (crawl.data?.facets ?? []).filter((facet) => facet.total > 0);

  return (
    <div className={styles.page} data-testid="user-contact-page">
      <Link className={styles.breadcrumb} to="/contacts">
        {t('contact.breadcrumb')}
      </Link>
      <header className={styles.header}>
        <span className={styles.avatarWrap}>
          {person.data.icon && (
            <img
              className={styles.avatar}
              src={person.data.icon}
              alt=""
              onError={(event) => event.currentTarget.remove()}
            />
          )}
          <span className={styles.avatarFallback} aria-hidden="true">
            {name.slice(0, 2).toUpperCase()}
          </span>
        </span>
        <div className={styles.identity}>
          <h1 tabIndex={-1} data-testid="user-contact-title">
            {name}
          </h1>
          <p>@{person.data.username || person.data.nsid}</p>
          <p className={styles.nsid}>{person.data.nsid}</p>
        </div>
        <div className={styles.count} data-testid="user-contact-photo-count">
          <strong>{new Intl.NumberFormat(locale).format(person.data.photos_count)}</strong>
          <span>{t('contact.photos-count')}</span>
        </div>
      </header>

      <section
        className={styles.crawl}
        aria-labelledby="user-contact-crawl-title"
        data-testid="user-contact-crawl"
      >
        <div className={styles.sectionHeading}>
          <h2 id="user-contact-crawl-title">{t('contact.crawl.title')}</h2>
          <p>{t('contact.crawl.scope')}</p>
        </div>
        {crawl.isError ? (
          <p role="status" className={styles.partial}>
            {t('contact.crawl.error')}
          </p>
        ) : selected.length === 0 ? (
          <p className={styles.muted}>{t('contact.crawl.empty')}</p>
        ) : (
          <ul className={styles.facets}>
            {selected.map((facet) => {
              const percent = facet.total === 0 ? 0 : Math.round((facet.done / facet.total) * 100);
              return (
                <li className={styles.facet} key={facet.facet}>
                  <div className={styles.facetLabel}>
                    <span>{facetName(facet.facet, t)}</span>
                    <span>
                      {t('contact.crawl.completed', { done: facet.done, total: facet.total })}
                    </span>
                  </div>
                  <div
                    className={styles.track}
                    role="progressbar"
                    aria-label={t('contact.crawl.progress', { facet: facetName(facet.facet, t) })}
                    aria-valuemin={0}
                    aria-valuemax={100}
                    aria-valuenow={percent}
                  >
                    <span style={{ width: `${String(percent)}%` }} />
                  </div>
                </li>
              );
            })}
          </ul>
        )}
      </section>

      <nav
        className={styles.tabs}
        aria-label={t('contact.tabs.label')}
        data-testid="user-contact-tabs"
      >
        {tabs.map((tab) => (
          <Link
            key={tab}
            to={`/contacts/${encodeURIComponent(id)}?tab=${tab}`}
            aria-current={selectedTab === tab ? 'page' : undefined}
            className={selectedTab === tab ? styles.activeTab : styles.tab}
            data-testid={`user-contact-tab-${tab}`}
          >
            {t(`contact.tabs.${tab}` as const)}
            {tab === 'photos'
              ? ` (${new Intl.NumberFormat(locale).format(person.data.photos_count)})`
              : ''}
          </Link>
        ))}
      </nav>

      <section className={styles.stub} data-testid="user-contact-tab-content">
        <h2>{t(`contact.tabs.${selectedTab}` as const)}</h2>
        <p>{t('contact.tab-stub')}</p>
      </section>
    </div>
  );
}
