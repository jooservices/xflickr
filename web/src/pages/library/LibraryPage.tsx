import { useEffect, useMemo, useState } from 'react';
import { useInfiniteQuery } from '@tanstack/react-query';
import { Link } from 'react-router';
import type { CatalogFavorite } from '../../shared/api/gen/models';
import { useSession } from '../../entities';
import { Icon, useI18n } from '../../shared';
import { LibraryApiError, loadFavorites } from '../../features/library/api';
import { PhotoViewer } from '../../features/photo-viewer';
import { PhotoGrid } from './PhotoGrid';
import { selectVisible } from './selection';
import { loadSelection, saveSelection } from './selectionStorage';
import styles from './LibraryPage.module.css';

function errorStatus(error: unknown): number | undefined {
  return error instanceof LibraryApiError ? error.status : undefined;
}

function uniqueFavorites(
  pages: Array<{ items: CatalogFavorite[] }> | undefined,
): CatalogFavorite[] {
  const items = pages?.flatMap((page) => page.items) ?? [];
  return [...new Map(items.map((item) => [item.photo.id, item])).values()];
}

export function LibraryPage() {
  const { locale, t } = useI18n();
  const favorites = useInfiniteQuery({
    queryKey: ['library', 'favorites'],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam, signal }) => loadFavorites(pageParam, signal),
    getNextPageParam: (page) => page.nextCursor,
    retry: false,
  });
  const [selectionMode, setSelectionMode] = useState(false);
  const { session } = useSession();
  const userId = session.status === 'authenticated' ? session.user.id : 0;
  const [selected, setSelected] = useState<Set<number>>(() => loadSelection(userId));
  const [viewer, setViewer] = useState<{ index: number; opener: HTMLButtonElement } | null>(null);
  const [contacts, setContacts] = useState<Set<string>>(() => new Set());
  const [contactSearch, setContactSearch] = useState('');
  const [debouncedContactSearch, setDebouncedContactSearch] = useState('');
  const [sizesReady, setSizesReady] = useState(false);
  const [captioned, setCaptioned] = useState(false);

  useEffect(() => {
    saveSelection(userId, selected);
  }, [selected, userId]);

  useEffect(() => {
    document.title = `${t('user.library.title')} · XFlickr`;
  }, [t, locale]);

  useEffect(() => {
    const timeout = window.setTimeout(
      () => setDebouncedContactSearch(contactSearch.trim().toLowerCase()),
      200,
    );
    return () => window.clearTimeout(timeout);
  }, [contactSearch]);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && selectionMode) setSelectionMode(false);
    };
    document.addEventListener('keydown', onKeyDown);
    return () => document.removeEventListener('keydown', onKeyDown);
  }, [selectionMode]);

  const photos = useMemo(() => uniqueFavorites(favorites.data?.pages), [favorites.data]);
  const contactOptions = useMemo(
    () =>
      [...new Set(photos.map(({ photo }) => photo.owner_nsid))].sort((left, right) =>
        left.localeCompare(right),
      ),
    [photos],
  );
  const filteredContactOptions = contactOptions.filter((contact) =>
    contact.toLowerCase().includes(debouncedContactSearch),
  );
  const visiblePhotos = useMemo(
    () =>
      photos.filter(({ photo }) => {
        if (contacts.size > 0 && !contacts.has(photo.owner_nsid)) return false;
        if (sizesReady && photo.sizes_status !== 'confirmed') return false;
        if (captioned && photo.ai_status !== 'ready') return false;
        return true;
      }),
    [captioned, contacts, photos, sizesReady],
  );
  const viewerPhotoIds = visiblePhotos.map(({ photo }) => photo.id);
  const number = new Intl.NumberFormat(locale);
  const filtered = contacts.size > 0 || sizesReady || captioned;
  const currentError = favorites.error;

  const toggleSelection = (photoId: number) => {
    setSelected((current) => {
      if (current.has(photoId)) {
        const next = new Set(current);
        next.delete(photoId);
        return next;
      }
      if (current.size >= 500) return current;
      return new Set(current).add(photoId);
    });
  };

  const toggleContact = (contact: string) => {
    setContacts((current) => {
      const next = new Set(current);
      if (next.has(contact)) next.delete(contact);
      else next.add(contact);
      return next;
    });
  };

  const clearFilters = () => {
    setContacts(new Set());
    setContactSearch('');
    setDebouncedContactSearch('');
    setSizesReady(false);
    setCaptioned(false);
  };

  const openPhoto = (photoId: number, opener: HTMLButtonElement) => {
    const index = viewerPhotoIds.indexOf(photoId);
    if (index >= 0) setViewer({ index, opener });
  };

  const status = errorStatus(currentError);
  const forbidden = status === 403;
  const initialError = favorites.isError && favorites.data === undefined && !forbidden;
  const loading = favorites.isPending;

  return (
    <section className={styles.page} data-testid="user-library-page">
      <header className={styles.pageHead}>
        <div>
          <h1 data-testid="user-library-heading" tabIndex={-1}>
            {t('user.library.title')}
          </h1>
          <p className={styles.lead} data-testid="user-library-total" aria-live="polite">
            {t('user.library.lead-loaded', {
              count: photos.length,
              formattedCount: number.format(photos.length),
            })}
          </p>
        </div>
      </header>

      {loading ? (
        <div className={styles.loading} aria-busy="true" data-testid="user-library-loading">
          <p role="status">{t('user.library.status.loading')}</p>
          <div className={styles.skeletonGrid} aria-hidden="true">
            {Array.from({ length: 12 }, (_, index) => (
              <span key={index} />
            ))}
          </div>
        </div>
      ) : forbidden ? (
        <section className={styles.state} data-testid="user-library-forbidden">
          <Icon name="lock" size={28} />
          <h2>{t('user.library.forbidden.title')}</h2>
          <p>{t('user.library.forbidden.text')}</p>
          <Link to="/" className={styles.stateLink} data-testid="user-library-forbidden-cta">
            {t('user.library.forbidden.cta')}
          </Link>
        </section>
      ) : initialError ? (
        <section className={styles.state} role="alert" data-testid="user-library-error">
          <Icon name="alert" size={28} />
          <h2>{t('user.library.error.title')}</h2>
          <p>{t('user.library.error.text')}</p>
          <button
            className={styles.secondaryButton}
            type="button"
            data-testid="user-library-retry"
            onClick={() => void favorites.refetch()}
          >
            {t('user.common.retry')}
          </button>
        </section>
      ) : (
        <div className={styles.layout}>
          <aside
            className={styles.filters}
            aria-label={t('user.library.filters.title')}
            data-testid="user-library-filters"
          >
            <h2>{t('user.library.filters.title')}</h2>
            <p className={styles.filterHint}>{t('user.library.filters.loaded-scope')}</p>
            <fieldset data-testid="user-library-filter-kind">
              <legend>{t('user.library.filters.kind')}</legend>
              <label className={styles.filterOption}>
                <input type="radio" name="library-kind" checked readOnly />
                <span>{t('user.library.filters.kind.favorites')}</span>
              </label>
            </fieldset>
            <fieldset data-testid="user-library-filter-contact">
              <legend>{t('user.library.filters.contact')}</legend>
              <input
                className={styles.contactSearch}
                type="search"
                value={contactSearch}
                aria-label={t('user.library.filters.contact-search')}
                data-testid="user-library-contact-search"
                onChange={(event) => setContactSearch(event.currentTarget.value)}
              />
              {filteredContactOptions.length === 0 ? (
                <p className={styles.filterHint}>{t('user.library.filters.contact-none')}</p>
              ) : (
                <div className={styles.contactOptions}>
                  {filteredContactOptions.map((contact) => (
                    <label className={styles.filterOption} key={contact}>
                      <input
                        type="checkbox"
                        checked={contacts.has(contact)}
                        onChange={() => toggleContact(contact)}
                      />
                      <span>{contact}</span>
                    </label>
                  ))}
                </div>
              )}
            </fieldset>
            <fieldset>
              <legend>{t('user.library.filters.readiness')}</legend>
              <label className={styles.filterOption}>
                <input
                  type="checkbox"
                  checked={sizesReady}
                  data-testid="user-library-filter-sizes"
                  onChange={(event) => setSizesReady(event.currentTarget.checked)}
                />
                <span>{t('user.library.filters.sizes')}</span>
              </label>
              <label className={styles.filterOption}>
                <input
                  type="checkbox"
                  checked={captioned}
                  data-testid="user-library-filter-captioned"
                  onChange={(event) => setCaptioned(event.currentTarget.checked)}
                />
                <span>{t('user.library.filters.captioned')}</span>
              </label>
            </fieldset>
            {filtered ? (
              <button
                className={styles.clearButton}
                type="button"
                data-testid="user-library-filter-clear"
                onClick={clearFilters}
              >
                {t('user.library.filters.clear')}
              </button>
            ) : null}
          </aside>

          <section className={styles.results} aria-label={t('user.library.results.label')}>
            {favorites.isRefetchError ? (
              <div className={styles.moreError} role="alert" data-testid="user-library-stale">
                <span>{t('user.library.status.stale')}</span>
                <button
                  className={styles.clearButton}
                  type="button"
                  data-testid="user-library-stale-retry"
                  disabled={favorites.isFetching}
                  onClick={() => void favorites.refetch()}
                >
                  {t('user.common.retry')}
                </button>
              </div>
            ) : null}
            <div className={styles.toolbar}>
              <p aria-live="polite">
                {t('user.library.results.count', {
                  matching: number.format(visiblePhotos.length),
                  loaded: number.format(photos.length),
                })}
              </p>
              <span className={styles.toolbarSpacer} />
              <button
                className={styles.secondaryButton}
                type="button"
                aria-pressed={selectionMode}
                data-testid="user-library-select-mode"
                onClick={() => setSelectionMode((active) => !active)}
              >
                {t(
                  selectionMode
                    ? 'user.library.action.done-selecting'
                    : 'user.library.action.select',
                )}
              </button>
              {selectionMode ? (
                <>
                  <button
                    className={styles.secondaryButton}
                    type="button"
                    data-testid="user-library-select-visible"
                    disabled={selected.size >= 500 || visiblePhotos.length === 0}
                    onClick={() =>
                      setSelected((current) =>
                        selectVisible(
                          current,
                          visiblePhotos.map(({ photo }) => photo.id),
                          500,
                        ),
                      )
                    }
                  >
                    {t('user.library.action.select-visible')}
                  </button>
                  <button
                    className={styles.clearButton}
                    type="button"
                    data-testid="user-library-clear-selection"
                    disabled={selected.size === 0}
                    onClick={() => setSelected(new Set())}
                  >
                    {t('user.library.action.clear-selection')}
                  </button>
                  <span
                    className={styles.srOnly}
                    role="status"
                    data-testid="user-library-selection-status"
                  >
                    {t('user.library.selection.status', { count: number.format(selected.size) })}
                  </span>
                </>
              ) : null}
            </div>

            {selected.size >= 500 ? (
              <p className={styles.limit}>{t('user.library.selection.limit')}</p>
            ) : null}

            {photos.length === 0 ? (
              favorites.hasNextPage ? null : (
                <section className={styles.state} data-testid="user-library-empty">
                  <Icon name="image" size={28} />
                  <h2>{t('user.library.empty.title')}</h2>
                  <p>{t('user.library.empty.text')}</p>
                </section>
              )
            ) : visiblePhotos.length === 0 ? (
              <section className={styles.filteredEmpty} data-testid="user-library-empty">
                <p>{t('user.library.empty.filtered')}</p>
                <button className={styles.clearButton} type="button" onClick={clearFilters}>
                  {t('user.library.filters.clear')}
                </button>
              </section>
            ) : (
              <PhotoGrid
                photos={visiblePhotos}
                selected={selected}
                selectionMode={selectionMode}
                selectionFull={selected.size >= 500}
                staleNotice={favorites.isRefetchError}
                onToggleSelected={toggleSelection}
                onOpenPhoto={openPhoto}
              />
            )}

            {favorites.isFetchNextPageError ? (
              <div className={styles.moreError} role="alert" data-testid="user-library-more-error">
                <span>{t('user.library.error.more')}</span>
                <button
                  className={styles.clearButton}
                  type="button"
                  data-testid="user-library-load-more-retry"
                  disabled={favorites.isFetching}
                  onClick={() => void favorites.fetchNextPage()}
                >
                  {t('user.common.retry')}
                </button>
              </div>
            ) : null}
            {favorites.hasNextPage ? (
              <div className={styles.loadMoreRow}>
                <button
                  className={styles.secondaryButton}
                  type="button"
                  data-testid="user-library-load-more"
                  disabled={favorites.isFetching}
                  aria-busy={favorites.isFetchingNextPage}
                  onClick={() => void favorites.fetchNextPage()}
                >
                  {favorites.isFetchingNextPage
                    ? t('user.library.status.loading')
                    : t('user.library.action.load-more')}
                </button>
              </div>
            ) : null}
          </section>
        </div>
      )}
      {viewer ? (
        <PhotoViewer
          photoIds={viewerPhotoIds}
          initialIndex={viewer.index}
          opener={viewer.opener}
          onClose={() => setViewer(null)}
        />
      ) : null}
    </section>
  );
}
