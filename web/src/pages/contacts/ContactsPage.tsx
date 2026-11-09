import { useInfiniteQuery, useQuery } from '@tanstack/react-query';
import { useEffect } from 'react';
import { Link } from 'react-router';
import { getMeCrawlSettings } from '../../shared/api/gen/crawl/crawl';
import { listPeople } from '../../shared/api/gen/catalog/catalog';
import type { CatalogPersonPage } from '../../shared/api/gen/models';
import { useI18n } from '../../shared';
import styles from './ContactsPage.module.css';

const PAGE_SIZE = 30;

type ApiError = Error & { status: number };

function apiError(status: number): ApiError {
  return Object.assign(new Error(`Contacts request failed with ${String(status)}`), { status });
}

async function loadPeople(
  cursor: string | undefined,
  signal: AbortSignal,
): Promise<CatalogPersonPage> {
  const response = await listPeople({ cursor, limit: PAGE_SIZE }, { signal });
  if (response.status !== 200) throw apiError(response.status);
  return response.data;
}

async function loadDepth(signal: AbortSignal): Promise<number> {
  const response = await getMeCrawlSettings({ signal });
  if (response.status !== 200) throw apiError(response.status);
  return response.data.depth;
}

function errorStatus(error: unknown): number | undefined {
  return typeof error === 'object' && error !== null && 'status' in error
    ? Number(error.status)
    : undefined;
}

export function ContactsPage() {
  const { t, locale } = useI18n();
  useEffect(() => {
    document.title = `${t('contacts.title')} · XFlickr`;
  }, [t]);
  const people = useInfiniteQuery({
    queryKey: ['contacts', 'people'],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam, signal }) => loadPeople(pageParam, signal),
    getNextPageParam: (page) => page.next_cursor,
    retry: false,
  });
  const depth = useQuery({
    queryKey: ['contacts', 'depth'],
    queryFn: ({ signal }) => loadDepth(signal),
    retry: false,
  });
  const loadMore = () => void people.fetchNextPage();

  const items = people.data?.pages.flatMap((page) => page.items) ?? [];
  const forbidden = errorStatus(people.error) === 403;
  const empty = people.isSuccess && items.length === 0;

  if (forbidden) {
    return (
      <div className={styles.page} data-testid="user-contacts-page">
        <h1 tabIndex={-1} data-testid="user-contacts-title">
          {t('contacts.title')}
        </h1>
        <section className={styles.state} data-testid="user-contacts-forbidden">
          <h2>{t('contacts.forbidden.title')}</h2>
          <p>{t('contacts.forbidden.text')}</p>
          <Link className={styles.button} to="/">
            {t('contacts.forbidden.back')}
          </Link>
        </section>
      </div>
    );
  }

  return (
    <div className={styles.page} data-testid="user-contacts-page">
      <header className={styles.heading}>
        <div>
          <h1 tabIndex={-1} data-testid="user-contacts-title">
            {t('contacts.title')}
          </h1>
          <p>{t('contacts.description')}</p>
        </div>
        <span className={styles.depth} data-testid="user-contacts-depth">
          {depth.data === undefined
            ? t('contacts.depth-unavailable')
            : t('contacts.depth', { depth: depth.data })}
        </span>
      </header>

      {people.isPending ? (
        <section className={styles.loading} aria-busy="true" data-testid="user-contacts-loading">
          <p role="status">{t('contacts.loading')}</p>
          <div className={styles.grid} aria-hidden="true">
            {Array.from({ length: 6 }, (_, index) => (
              <div className={styles.skeleton} key={index} />
            ))}
          </div>
        </section>
      ) : people.isError ? (
        <section className={styles.state} role="alert" data-testid="user-contacts-error">
          <h2>{t('contacts.error.title')}</h2>
          <p>{t('contacts.error.text')}</p>
          <button
            className={styles.button}
            onClick={() => void people.refetch()}
            data-testid="user-contacts-retry"
          >
            {t('contacts.error.retry')}
          </button>
        </section>
      ) : empty ? (
        <section className={styles.state} data-testid="user-contacts-empty">
          <h2>{t('contacts.empty.title')}</h2>
          <p>{t('contacts.empty.text')}</p>
          <Link
            className={`${styles.button} ${styles.primary}`}
            to="/onboarding"
            data-testid="user-contacts-empty-setup"
          >
            {t('contacts.empty.setup')}
          </Link>
        </section>
      ) : (
        <>
          {depth.isError && (
            <p className={styles.partial} role="status">
              {t('contacts.depth-unavailable')}
            </p>
          )}
          <ul
            className={styles.grid}
            aria-label={t('contacts.list.label')}
            data-testid="user-contacts-list"
          >
            {items.map((person) => {
              const name = person.realname || person.username || person.nsid;
              return (
                <li className={styles.card} data-testid="user-contacts-card" key={person.nsid}>
                  <Link
                    className={styles.person}
                    to={`/contacts/${encodeURIComponent(person.nsid)}`}
                    aria-label={t('contacts.card.open', { name })}
                  >
                    <span className={styles.avatarWrap}>
                      {person.icon && (
                        <img
                          className={styles.avatar}
                          src={person.icon}
                          alt=""
                          onError={(event) => event.currentTarget.remove()}
                        />
                      )}
                      <span className={styles.avatarFallback} aria-hidden="true">
                        {name.slice(0, 2).toUpperCase()}
                      </span>
                    </span>
                    <span className={styles.personInfo}>
                      <strong>{name}</strong>
                      <span>@{person.username || person.nsid}</span>
                      <span>
                        {t('contacts.card.photos', {
                          count: new Intl.NumberFormat(locale).format(person.photos_count),
                        })}
                      </span>
                    </span>
                  </Link>
                </li>
              );
            })}
          </ul>
          {people.isFetchNextPageError && (
            <p className={styles.partial} role="alert">
              {t('contacts.more-error')}{' '}
              <button className={styles.linkButton} onClick={loadMore}>
                {t('contacts.error.retry')}
              </button>
            </p>
          )}
          {people.hasNextPage && (
            <button
              className={styles.button}
              onClick={loadMore}
              disabled={people.isFetchingNextPage}
              data-testid="user-contacts-load-more"
            >
              {people.isFetchingNextPage ? t('contacts.loading.more') : t('contacts.load-more')}
            </button>
          )}
        </>
      )}
    </div>
  );
}
