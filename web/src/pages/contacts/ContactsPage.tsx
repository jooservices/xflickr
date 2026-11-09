import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { InfiniteData } from '@tanstack/react-query';
import { useCallback, useEffect, useState } from 'react';
import { Link, useSearchParams } from 'react-router';
import {
  getMeCrawlSettings,
  listMeCrawlInterests,
  starMyContact,
} from '../../shared/api/gen/crawl/crawl';
import type {
  CrawlInterest,
  CrawlInterestPage,
  ListMeCrawlInterestsFilter,
  ListMeCrawlInterestsSort,
} from '../../shared/api/gen/models';
import { useI18n } from '../../shared';
import type { MessageKey, MessageParams } from '../../shared';
import styles from './ContactsPage.module.css';

const PAGE_SIZE = 30;
const filters: ListMeCrawlInterestsFilter[] = ['all', 'starred', 'crawling', 'needs_attention'];
const sorts: ListMeCrawlInterestsSort[] = ['updated', 'name', 'photos'];

type ApiError = Error & { status: number };
type StarVariables = { nsid: string; starred: boolean };

function apiError(status: number): ApiError {
  return Object.assign(new Error(`Contacts request failed with ${String(status)}`), { status });
}

function errorStatus(error: unknown): number | undefined {
  return typeof error === 'object' && error !== null && 'status' in error
    ? Number(error.status)
    : undefined;
}

function selectFilter(value: string | null): ListMeCrawlInterestsFilter {
  return filters.includes(value as ListMeCrawlInterestsFilter)
    ? (value as ListMeCrawlInterestsFilter)
    : 'all';
}

function selectSort(value: string | null): ListMeCrawlInterestsSort {
  return sorts.includes(value as ListMeCrawlInterestsSort)
    ? (value as ListMeCrawlInterestsSort)
    : 'updated';
}

async function loadInterests(
  filter: ListMeCrawlInterestsFilter,
  q: string,
  sort: ListMeCrawlInterestsSort,
  cursor: string | undefined,
  signal: AbortSignal,
): Promise<CrawlInterestPage> {
  const response = await listMeCrawlInterests(
    { filter, q: q || undefined, sort, cursor, limit: PAGE_SIZE },
    { signal },
  );
  if (response.status !== 200) throw apiError(response.status);
  return response.data;
}

function ContactCard({
  contact,
  locale,
  onStar,
  starPending,
  t,
}: {
  contact: CrawlInterest;
  locale: string;
  onStar: (contact: CrawlInterest) => void;
  starPending: boolean;
  t: (key: string, options?: MessageParams) => string;
}) {
  const name = contact.name || contact.nsid;
  const percent =
    contact.photos_total > 0
      ? Math.min(100, Math.round((contact.photos_crawled / contact.photos_total) * 100))
      : 0;

  return (
    <li className={styles.card} data-testid="user-contact-card" key={contact.nsid}>
      <div className={styles.mosaic} aria-hidden="true">
        {(contact.recent_photos ?? []).slice(0, 3).map((photo) => (
          <img
            className={styles.mosaicPhoto}
            key={photo.id}
            src={photo.thumbnail_url || undefined}
            alt=""
            onError={(event) => event.currentTarget.remove()}
          />
        ))}
      </div>
      <div className={styles.cardBody}>
        <div className={styles.cardTop}>
          <Link
            className={styles.person}
            to={`/contacts/${encodeURIComponent(contact.nsid)}`}
            aria-label={t('contacts.card.open', { name })}
          >
            <span className={styles.avatarWrap}>
              {contact.avatar_url && (
                <img
                  className={styles.avatar}
                  src={contact.avatar_url}
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
              <span>@{contact.username || contact.nsid}</span>
              <span>{t('contacts.card.depth', { depth: contact.depth })}</span>
            </span>
          </Link>
          <button
            className={styles.star}
            type="button"
            aria-pressed={contact.starred}
            aria-label={t(contact.starred ? 'contacts.star.remove' : 'contacts.star.add', { name })}
            disabled={starPending}
            data-testid="user-contact-star"
            onClick={() => onStar(contact)}
          >
            {contact.starred ? '★' : '☆'}
          </button>
        </div>
        <div className={styles.metrics}>
          <span>
            {t('contacts.card.photos', {
              count: new Intl.NumberFormat(locale).format(contact.photos_total),
            })}
          </span>
          <span>
            {t('contacts.card.favorites', {
              count: new Intl.NumberFormat(locale).format(contact.favorites_count),
            })}
          </span>
          <span>{t('contacts.card.sizes', { percent: contact.sizes_percent })}</span>
        </div>
        <div className={styles.statusRow}>
          <span className={styles.status} data-testid="user-contact-status">
            {t(`contacts.status.${contact.status}`)}
          </span>
          <span>{percent}%</span>
        </div>
        <div
          className={styles.progress}
          role="progressbar"
          aria-label={t('contacts.card.progress', { percent })}
          aria-valuemin={0}
          aria-valuemax={100}
          aria-valuenow={percent}
          data-testid="user-contact-progress"
        >
          <span style={{ width: `${String(percent)}%` }} />
        </div>
      </div>
    </li>
  );
}

function ContactsSearch({
  query,
  onCommit,
  t,
}: {
  query: string;
  onCommit: (query: string) => void;
  t: (key: string, options?: MessageParams) => string;
}) {
  const [value, setValue] = useState(query);

  useEffect(() => {
    const normalized = value.trim();
    if (normalized === query) return;
    const timer = window.setTimeout(() => onCommit(normalized), 250);
    return () => window.clearTimeout(timer);
  }, [onCommit, query, value]);

  return (
    <label className={styles.searchLabel}>
      {t('contacts.search.label')}
      <input
        className={styles.search}
        type="search"
        value={value}
        placeholder={t('contacts.search.placeholder')}
        onChange={(event) => setValue(event.currentTarget.value)}
        data-testid="user-contacts-search"
      />
    </label>
  );
}

export function ContactsPage() {
  const { t: translate, locale } = useI18n();
  const t = useCallback(
    (key: string, params?: MessageParams) =>
      translate((key.startsWith('contacts.') ? key : `contacts.${key}`) as MessageKey, params),
    [translate],
  );
  const [searchParams, setSearchParams] = useSearchParams();
  const queryClient = useQueryClient();
  const filter = selectFilter(searchParams.get('filter'));
  const sort = selectSort(searchParams.get('sort'));
  const q = searchParams.get('q')?.trim() ?? '';
  const [starError, setStarError] = useState(false);

  useEffect(() => {
    document.title = `${t('contacts.title')} · XFlickr`;
  }, [t]);

  const commitSearch = useCallback(
    (normalized: string) => {
      setSearchParams(
        (current) => {
          const next = new URLSearchParams(current);
          if (normalized) next.set('q', normalized);
          else next.delete('q');
          next.delete('cursor');
          return next;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  const queryKey = ['contacts', 'interests', filter, q, sort] as const;
  const interests = useInfiniteQuery({
    queryKey,
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam, signal }) => loadInterests(filter, q, sort, pageParam, signal),
    getNextPageParam: (page) => page.next_cursor ?? undefined,
    retry: false,
  });
  const depth = useQuery({
    queryKey: ['contacts', 'depth'],
    queryFn: async ({ signal }) => {
      const response = await getMeCrawlSettings({ signal });
      if (response.status !== 200) throw apiError(response.status);
      return response.data.depth;
    },
    retry: false,
  });
  const star = useMutation({
    mutationFn: async ({ nsid, starred }: StarVariables) => {
      const response = await starMyContact(nsid, { starred });
      if (response.status !== 204) throw apiError(response.status);
    },
    onMutate: async ({ nsid, starred }) => {
      setStarError(false);
      await queryClient.cancelQueries({ queryKey });
      const previous = queryClient.getQueryData<InfiniteData<CrawlInterestPage>>(queryKey);
      queryClient.setQueryData<InfiniteData<CrawlInterestPage>>(queryKey, (current) => {
        if (!current) return current;
        return {
          ...current,
          pages: current.pages.map((page) => ({
            ...page,
            items: page.items.map((item) => (item.nsid === nsid ? { ...item, starred } : item)),
          })),
        };
      });
      return { previous };
    },
    onError: (_error, _variables, context) => {
      if (context?.previous) queryClient.setQueryData(queryKey, context.previous);
      setStarError(true);
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey }),
  });

  const items = interests.data?.pages.flatMap((page) => page.items) ?? [];
  const counts = interests.data?.pages[0]?.counts;
  const forbidden = errorStatus(interests.error) === 403;
  const initialError = interests.isError && !interests.data && !forbidden;
  const refreshError =
    interests.isError && Boolean(interests.data) && !interests.isFetchNextPageError;
  const empty = interests.isSuccess && items.length === 0;
  const updateParams = (values: Partial<Record<'filter' | 'sort' | 'q', string | null>>) => {
    setSearchParams((current) => {
      const next = new URLSearchParams(current);
      for (const [key, value] of Object.entries(values)) {
        if (value === null) next.delete(key);
        else if (value !== undefined) next.set(key, value);
      }
      next.delete('cursor');
      return next;
    });
  };

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
          <p>{t('contacts.description', { depth: depth.data ?? '—' })}</p>
        </div>
      </header>

      <section className={styles.controls} aria-label={t('contacts.filter.label')}>
        <ContactsSearch key={q} query={q} onCommit={commitSearch} t={t} />
        <div className={styles.filterGroup} role="group" aria-label={t('contacts.filter.label')}>
          {filters.map((value) => (
            <button
              className={value === filter ? styles.activeFilter : styles.filter}
              key={value}
              type="button"
              aria-pressed={value === filter}
              onClick={() => updateParams({ filter: value })}
              data-testid={`user-contacts-filter-${value}`}
            >
              {t(`contacts.filter.${value}`)}
              {counts && <span className={styles.filterCount}>{counts[value]}</span>}
            </button>
          ))}
        </div>
        <label className={styles.sortLabel}>
          {t('contacts.sort.label')}
          <select
            value={sort}
            onChange={(event) => updateParams({ sort: event.currentTarget.value })}
            data-testid="user-contacts-sort"
          >
            {sorts.map((value) => (
              <option key={value} value={value}>
                {t(`contacts.sort.${value}`)}
              </option>
            ))}
          </select>
        </label>
      </section>

      {refreshError && (
        <p className={styles.partial} role="alert" data-testid="user-contacts-refresh-error">
          {t('contacts.partial.title')}{' '}
          <button
            className={styles.linkButton}
            type="button"
            onClick={() => void interests.refetch()}
            data-testid="user-contacts-refresh-retry"
          >
            {t('contacts.partial.retry')}
          </button>
        </p>
      )}

      {interests.isPending ? (
        <section className={styles.loading} aria-busy="true" data-testid="user-contacts-loading">
          <p role="status">{t('contacts.loading')}</p>
          <div className={styles.grid} aria-hidden="true">
            {Array.from({ length: 6 }, (_, index) => (
              <div className={styles.skeleton} key={index} />
            ))}
          </div>
        </section>
      ) : initialError ? (
        <section className={styles.state} role="alert" data-testid="user-contacts-error">
          <h2>{t('contacts.error.title')}</h2>
          <p>{t('contacts.error.text')}</p>
          <button
            className={styles.button}
            onClick={() => void interests.refetch()}
            data-testid="user-contacts-retry"
          >
            {t('contacts.error.retry')}
          </button>
        </section>
      ) : empty ? (
        <section className={styles.state} data-testid="user-contacts-empty">
          <h2>
            {t(q || filter !== 'all' ? 'contacts.empty.filtered.title' : 'contacts.empty.title')}
          </h2>
          <p>{t(q || filter !== 'all' ? 'contacts.empty.filtered.text' : 'contacts.empty.text')}</p>
          {q || filter !== 'all' ? (
            <button
              className={styles.button}
              onClick={() => updateParams({ filter: 'all', q: null })}
            >
              {t('contacts.empty.clear')}
            </button>
          ) : (
            <Link
              className={`${styles.button} ${styles.primary}`}
              to="/onboarding"
              data-testid="user-contacts-empty-setup"
            >
              {t('contacts.empty.setup')}
            </Link>
          )}
        </section>
      ) : (
        <>
          {(interests.isRefetchError || depth.isError || starError) && (
            <p className={styles.partial} role="alert" data-testid="user-contacts-stale">
              {starError ? t('contacts.star.error') : t('contacts.partial.title')}{' '}
              <button className={styles.linkButton} onClick={() => void interests.refetch()}>
                {t('contacts.partial.retry')}
              </button>
            </p>
          )}
          <ul
            className={styles.grid}
            aria-label={t('contacts.list.label')}
            data-testid="user-contacts-list"
          >
            {items.map((contact) => (
              <ContactCard
                contact={contact}
                key={contact.nsid}
                locale={locale}
                onStar={(value) => star.mutate({ nsid: value.nsid, starred: !value.starred })}
                starPending={star.isPending}
                t={t}
              />
            ))}
          </ul>
          {interests.isFetchNextPageError && (
            <p className={styles.partial} role="alert" data-testid="user-contacts-more-error">
              {t('contacts.more-error')}{' '}
              <button
                className={styles.linkButton}
                onClick={() => void interests.fetchNextPage()}
                data-testid="user-contacts-retry-more"
              >
                {t('contacts.error.retry')}
              </button>
            </p>
          )}
          {interests.hasNextPage && (
            <button
              className={styles.button}
              onClick={() => void interests.fetchNextPage()}
              disabled={interests.isFetchingNextPage}
              data-testid="user-contacts-load-more"
            >
              {interests.isFetchingNextPage ? t('contacts.loading.more') : t('contacts.load-more')}
            </button>
          )}
        </>
      )}
    </div>
  );
}
