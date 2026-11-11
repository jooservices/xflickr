import { useInfiniteQuery } from '@tanstack/react-query';
import { useEffect, useMemo, useRef } from 'react';
import { useLocation, useNavigate } from 'react-router';
import type { MyAlbumPage } from '../../shared/api/gen/models';
import { listMyAlbums } from '../../shared/api/gen/catalog/catalog';
import { EmptyState, ForbiddenState, Icon, useI18n, useLoadingTiming } from '../../shared';
import styles from './AlbumsPage.module.css';

const PAGE_SIZE = 50;
const albumQueryKey = ['catalog', 'my-albums'] as const;

class AlbumsRequestError extends Error {
  constructor(readonly status: number) {
    super('Albums request failed');
    this.name = 'AlbumsRequestError';
  }
}

function responseStatus(error: unknown): number | undefined {
  return error instanceof AlbumsRequestError ? error.status : undefined;
}

async function loadAlbums(cursor?: string): Promise<MyAlbumPage> {
  const response = await listMyAlbums({ cursor, limit: PAGE_SIZE });
  if (response.status !== 200) throw new AlbumsRequestError(response.status);
  return response.data;
}

function AlbumCard({ album, locale }: { album: MyAlbumPage['items'][number]; locale: string }) {
  const updatedAt = album.updated_at ? new Date(album.updated_at) : null;
  const updatedAtLabel = updatedAt && !Number.isNaN(updatedAt.getTime())
    ? new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeZone: 'UTC' }).format(updatedAt)
    : null;

  return (
    <article
      className={styles.albumCard}
      data-testid="album-card"
      data-album-id={album.id}
    >
      <span className={styles.stack} aria-hidden="true">
        <span className={styles.coverBack} />
        <span className={styles.coverMiddle} />
        <span className={styles.coverFront}>
          <Icon name="album" size={30} />
        </span>
      </span>
      <h2 className={styles.title}>{album.title || album.id}</h2>
      <span className={styles.meta}>
        <span>{album.owner_nsid}</span>
        {updatedAtLabel ? <time dateTime={album.updated_at}>{updatedAtLabel}</time> : null}
      </span>
    </article>
  );
}

/** Lists contact albums with the generated, cursor-based REST client. */
export function AlbumsPage() {
  const { t, locale } = useI18n();
  const navigate = useNavigate();
  const location = useLocation();
  const headingRef = useRef<HTMLHeadingElement>(null);
  const albums = useInfiniteQuery({
    queryKey: albumQueryKey,
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) => loadAlbums(pageParam),
    getNextPageParam: (lastPage) => lastPage.next_cursor,
    retry: false,
  });
  const skeletonVisible = useLoadingTiming(albums.isPending, 200);
  const contact = new URLSearchParams(location.search).get('contact') ?? '';
  const allAlbums = useMemo(
    () => albums.data?.pages.flatMap((page) => page.items) ?? [],
    [albums.data],
  );
  const owners = useMemo(
    () =>
      [...new Set(allAlbums.map((album) => album.owner_nsid))].sort((left, right) =>
        left.localeCompare(right),
      ),
    [allAlbums],
  );
  const visibleAlbums = contact
    ? allAlbums.filter((album) => album.owner_nsid === contact)
    : allAlbums;
  const unavailableContact =
    (location.state as { albumsContactUnavailable?: unknown } | null)?.albumsContactUnavailable ===
    true;
  const forbidden = responseStatus(albums.error) === 403;
  const unauthenticated = responseStatus(albums.error) === 401;
  const initialError =
    albums.isError && albums.data === undefined && !forbidden && !unauthenticated;
  const partialError = albums.isFetchNextPageError && !forbidden;

  useEffect(() => {
    document.title = `${t('albums.title')} · XFlickr`;
    headingRef.current?.focus();
  }, [t]);

  useEffect(() => {
    if (
      !contact ||
      albums.isPending ||
      albums.isError ||
      albums.hasNextPage ||
      owners.includes(contact)
    )
      return;
    const next = new URLSearchParams(location.search);
    next.delete('contact');
    const currentState =
      typeof location.state === 'object' && location.state !== null ? location.state : {};
    void navigate(
      {
        pathname: location.pathname,
        search: next.size === 0 ? '' : `?${next.toString()}`,
      },
      { replace: true, state: { ...currentState, albumsContactUnavailable: true } },
    );
  }, [
    albums.isError,
    albums.hasNextPage,
    albums.isPending,
    contact,
    location.pathname,
    location.search,
    location.state,
    navigate,
    owners,
  ]);

  const retry = () => {
    if (partialError) void albums.fetchNextPage();
    else void albums.refetch();
  };

  const changeContact = (value: string) => {
    const next = new URLSearchParams(location.search);
    if (value) next.set('contact', value);
    else next.delete('contact');
    void navigate(
      {
        pathname: location.pathname,
        search: next.size === 0 ? '' : `?${next.toString()}`,
      },
      { state: null },
    );
  };

  const pageState = forbidden
    ? 'forbidden'
    : initialError
      ? 'error'
      : albums.isPending
        ? 'loading'
        : 'ready';

  if (forbidden) {
    return (
      <div className={styles.page} data-testid="albums-page" data-state="forbidden">
        <ForbiddenState
          testId="albums-forbidden"
          ctaTestId="albums-forbidden-home"
          onHome={() => void navigate('/')}
        />
      </div>
    );
  }

  if (unauthenticated) return null;

  return (
    <section
      className={styles.page}
      aria-busy={albums.isPending || albums.isFetchingNextPage}
      data-testid="albums-page"
      data-state={pageState}
    >
      <header className={styles.header}>
        <div>
          <h1 ref={headingRef} tabIndex={-1} className={styles.heading}>
            {t('albums.title')}
          </h1>
          <p className={styles.description}>{t('albums.description')}</p>
        </div>
        <div className={styles.filterWrap}>
          <label htmlFor="albums-contact-filter">{t('albums.filter.label')}</label>
          <select
            id="albums-contact-filter"
            className={styles.filter}
            value={contact}
            onChange={(event) => changeContact(event.currentTarget.value)}
            data-testid="albums-filter"
          >
            <option value="">{t('albums.filter.all')}</option>
            {contact && !owners.includes(contact) ? (
              <option value={contact}>{contact}</option>
            ) : null}
            {owners.map((owner) => (
              <option key={owner} value={owner}>
                {owner}
              </option>
            ))}
          </select>
          {albums.isFetching && albums.data !== undefined ? (
            <span className={styles.busy} role="status">
              {t('albums.loading.label')}
            </span>
          ) : null}
        </div>
      </header>

      {unavailableContact ? (
        <p className={styles.notice} role="status">
          {t('albums.filter.unavailable')}
        </p>
      ) : null}

      {initialError ? (
        <div className={styles.state} role="alert" data-testid="albums-error">
          <Icon name="alert" size={30} />
          <h2>{t('albums.error.title')}</h2>
          <p>{t('albums.error.body')}</p>
          <button
            type="button"
            className={styles.action}
            onClick={retry}
            data-testid="albums-retry"
          >
            {t('common.retry')}
          </button>
        </div>
      ) : null}

      {albums.isPending && skeletonVisible ? (
        <>
          <p className={styles.loadingLabel} role="status">
            {t('albums.loading.label')}
          </p>
          <div className={styles.loading} aria-hidden="true" data-testid="albums-loading">
            {Array.from({ length: 6 }, (_, index) => (
              <div className={styles.skeletonCard} key={index} />
            ))}
          </div>
        </>
      ) : null}

      {!albums.isPending && !initialError && allAlbums.length === 0 ? (
        <EmptyState
          testId="albums"
          icon="album"
          title={t('albums.empty.title')}
          text={t('albums.empty.body')}
        />
      ) : null}

      {!albums.isPending && !initialError && visibleAlbums.length > 0 ? (
        <div className={styles.grid} data-testid="albums-grid">
          {visibleAlbums.map((album) => (
            <AlbumCard key={album.id} album={album} locale={locale} />
          ))}
        </div>
      ) : null}

      {partialError ? (
        <div className={styles.partial} role="status" data-testid="albums-partial">
          <span>{t('albums.partial.message')}</span>
          <button
            type="button"
            className={styles.action}
            onClick={retry}
            data-testid="albums-retry"
          >
            {t('common.retry')}
          </button>
        </div>
      ) : null}
      {albums.hasNextPage && !partialError ? (
        <div className={styles.moreWrap}>
          <button
            type="button"
            className={styles.action}
            onClick={() => void albums.fetchNextPage()}
            disabled={albums.isFetchingNextPage}
            data-testid="albums-load-more"
          >
            {albums.isFetchingNextPage ? t('albums.loading.label') : t('albums.action.more')}
          </button>
        </div>
      ) : null}
    </section>
  );
}
