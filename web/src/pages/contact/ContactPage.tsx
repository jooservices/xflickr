import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
  type InfiniteData,
} from '@tanstack/react-query';
import { useEffect, useRef, useState } from 'react';
import { Link, useParams, useSearchParams } from 'react-router';
import {
  getMeContact,
  getMeContactAbout,
  listMeContactAlbums,
  listMeContactFavorites,
  listMeContactGalleries,
  listMeContactGroups,
  listMeContactPhotos,
  setMeContactGroupPoolCrawl,
} from '../../shared/api/gen/catalog/catalog';
import {
  pauseCrawlContact,
  prioritiseMeContactSizes,
  resumeCrawlContact,
  selectMyContact,
  starMyContact,
} from '../../shared/api/gen/crawl/crawl';
import { createDownload } from '../../shared/api/gen/default/default';
import type {
  ContactAbout,
  ContactAlbumPage,
  ContactDetail,
  ContactGalleryPage,
  ContactGroupPage,
  ContactFavoritePage,
  ContactPhotoPage,
  CreateDownloadRequestKind,
} from '../../shared/api/gen/models';
import type { MessageKey, Translate } from '../../shared';
import { useI18n } from '../../shared';
import { useDialog } from '../../shared/ui/useDialog';
import styles from './ContactPage.module.css';

const PAGE_SIZE = 30;
const DOWNLOAD_PAGE_SIZE = 100;
const DOWNLOAD_BATCH_SIZE = 500;
type Tab = 'photos' | 'favorites' | 'albums' | 'galleries' | 'groups' | 'about';
const tabs: Tab[] = ['photos', 'favorites', 'albums', 'galleries', 'groups', 'about'];

type ApiError = Error & { status: number };
type DownloadScope = 'contact' | 'favorites';
type ContactAccessFailure = { nsid: string; status: 403 | 404 };

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

function statusName(status: string, t: (key: MessageKey) => string): string {
  switch (status) {
    case 'pending':
      return t('contact.crawl.status.pending');
    case 'running':
      return t('contact.crawl.status.running');
    case 'complete':
      return t('contact.crawl.status.complete');
    case 'partial':
      return t('contact.crawl.status.partial');
    case 'failed':
      return t('contact.crawl.status.failed');
    case 'paused':
      return t('contact.crawl.status.paused');
    default:
      return t('contact.value.unavailable');
  }
}

function apiPage<T>(response: { status: number; data: unknown }): T {
  if (response.status !== 200) throw apiError(response.status);
  return response.data as T;
}

async function apiContactPage<T>(
  response: Promise<{ status: number; data: unknown }>,
  nsid: string,
  onAccessFailure: (failure: ContactAccessFailure) => void,
): Promise<T> {
  try {
    return apiPage<T>(await response);
  } catch (error) {
    const status = errorStatus(error);
    if (status === 403 || status === 404) onAccessFailure({ nsid, status });
    throw error;
  }
}

async function contactDownloadPhotoIDs(
  scope: DownloadScope,
  nsid: string,
  onAccessFailure: (failure: ContactAccessFailure) => void,
): Promise<number[]> {
  const ids: number[] = [];
  let cursor: string | undefined;
  do {
    const response =
      scope === 'favorites'
        ? await listMeContactFavorites(nsid, { cursor, limit: DOWNLOAD_PAGE_SIZE })
        : await listMeContactPhotos(nsid, { cursor, limit: DOWNLOAD_PAGE_SIZE });
    const page = await apiContactPage<ContactPhotoPage | ContactFavoritePage>(
      Promise.resolve(response),
      nsid,
      onAccessFailure,
    );
    for (const item of page.items) {
      const id = Number(item.id);
      if (!Number.isSafeInteger(id) || id <= 0) throw apiError(400);
      ids.push(id);
    }
    cursor = page.next_cursor ?? undefined;
  } while (cursor);
  return [...new Set(ids)];
}

function ContactFacets({ contact, t }: { contact: ContactDetail; t: Translate }) {
  return (
    <ul className={styles.facets}>
      {contact.facets.map((facet) => {
        const percent =
          facet.total && facet.total > 0
            ? Math.min(100, Math.round((facet.completed / facet.total) * 100))
            : 0;
        return (
          <li
            className={styles.facet}
            key={facet.name}
            data-testid={`user-contact-photos-facet-${facet.name}`}
          >
            <div className={styles.facetLabel}>
              <span>{facetName(facet.name, t)}</span>
              <span>
                {facet.total === null
                  ? statusName(facet.status, t)
                  : t('contact.crawl.facet-progress', {
                      done: facet.completed,
                      total: facet.total,
                      status: statusName(facet.status, t),
                    })}
              </span>
            </div>
            <div
              className={styles.track}
              role="progressbar"
              aria-label={facetName(facet.name, t)}
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
  );
}

function DownloadDialog({
  scope,
  nsid,
  onClose,
  onDone,
  onAccessFailure,
  returnFocus,
  t,
}: {
  scope: DownloadScope;
  nsid: string;
  onClose: () => void;
  onDone: (batchCount: number) => void;
  onAccessFailure: (failure: ContactAccessFailure) => void;
  returnFocus: () => HTMLElement | null;
  t: Translate;
}) {
  const [error, setError] = useState(false);
  const [pending, setPending] = useState(false);
  const [complete, setComplete] = useState(false);
  const [queuedBatchCount, setQueuedBatchCount] = useState(0);
  const queuedPhotoIDs = useRef(new Set<number>());
  const queuedBatches = useRef(0);
  const dialogRef = useRef<HTMLElement>(null);
  const closeRef = useRef<HTMLButtonElement>(null);

  useDialog(dialogRef, onClose, () => closeRef.current?.focus(), true, returnFocus);

  const submit = async () => {
    setError(false);
    setPending(true);
    try {
      const photoIDs = await contactDownloadPhotoIDs(scope, nsid, onAccessFailure);
      if (photoIDs.length === 0) throw apiError(404);
      for (let start = 0; start < photoIDs.length; start += DOWNLOAD_BATCH_SIZE) {
        const batch = photoIDs
          .slice(start, start + DOWNLOAD_BATCH_SIZE)
          .filter((photoID) => !queuedPhotoIDs.current.has(photoID));
        if (batch.length === 0) continue;
        const kind: CreateDownloadRequestKind = scope;
        const response = await createDownload({ kind, subject: nsid, photo_ids: batch });
        if (response.status !== 201) throw apiError(response.status);
        for (const photoID of batch) queuedPhotoIDs.current.add(photoID);
        queuedBatches.current += 1;
      }
      setQueuedBatchCount(queuedBatches.current);
      setComplete(true);
      onDone(queuedBatches.current);
    } catch {
      setQueuedBatchCount(queuedBatches.current);
      setError(true);
    } finally {
      setPending(false);
    }
  };

  return (
    <div
      className={styles.overlay}
      role="presentation"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget) onClose();
      }}
    >
      <section
        ref={dialogRef}
        className={styles.dialog}
        role="dialog"
        aria-modal="true"
        aria-labelledby="contact-download-title"
        data-testid="contact-download-modal"
      >
        <button
          className={styles.dialogClose}
          type="button"
          onClick={onClose}
          ref={closeRef}
          aria-label={t('contact.download.close')}
        />
        <h2 id="contact-download-title">
          {complete
            ? t(
                queuedBatchCount === 1
                  ? 'contact.download.success.title'
                  : 'contact.download.success.batches.title',
                { count: queuedBatchCount },
              )
            : t('contact.download.title')}
        </h2>
        {complete ? (
          <p role="status">
            {t(
              queuedBatchCount === 1
                ? 'contact.download.success.text'
                : 'contact.download.success.batches',
              { count: queuedBatchCount },
            )}
          </p>
        ) : (
          <>
            <p>
              {t(
                scope === 'contact'
                  ? 'contact.download.contact.text'
                  : 'contact.download.favorites.text',
              )}
            </p>
            {error && (
              <p className={styles.actionError} role="alert">
                {t('contact.download.error')}
              </p>
            )}
            <div className={styles.dialogActions}>
              <button className={styles.button} type="button" onClick={onClose}>
                {t('contact.download.cancel')}
              </button>
              <button
                className={styles.button}
                type="button"
                onClick={() => void submit()}
                disabled={pending}
                data-testid="download-modal-submit"
              >
                {pending ? t('contact.download.pending') : t('contact.download.submit')}
              </button>
            </div>
          </>
        )}
        {complete && (
          <button className={styles.button} type="button" onClick={onClose}>
            {t('contact.download.close')}
          </button>
        )}
      </section>
    </div>
  );
}

export function ContactPage() {
  const { id = '' } = useParams();
  const { t, locale } = useI18n();
  const [searchParams] = useSearchParams();
  const queryClient = useQueryClient();
  const [downloadScope, setDownloadScope] = useState<DownloadScope | null>(null);
  const [lightboxPhoto, setLightboxPhoto] = useState<{ title: string; url: string | null } | null>(
    null,
  );
  const [actionMessage, setActionMessage] = useState('');
  const [actionError, setActionError] = useState(false);
  const [accessFailure, setAccessFailure] = useState<ContactAccessFailure | null>(null);
  const lightboxRef = useRef<HTMLElement>(null);
  const lightboxCloseRef = useRef<HTMLButtonElement>(null);
  const downloadOpenerRef = useRef<HTMLElement | null>(null);
  const lightboxOpenerRef = useRef<HTMLElement | null>(null);
  const selectedTab = tabs.find((tab) => tab === searchParams.get('tab')) ?? 'photos';
  const contactBlocked = accessFailure?.nsid === id;
  useDialog(
    lightboxRef,
    () => setLightboxPhoto(null),
    () => lightboxCloseRef.current?.focus(),
    lightboxPhoto !== null,
    () => lightboxOpenerRef.current,
  );
  const personKey = ['contacts', 'detail', id] as const;
  const detail = useQuery({
    queryKey: personKey,
    queryFn: async ({ signal }) => apiPage<ContactDetail>(await getMeContact(id, { signal })),
    retry: false,
    enabled: id.length > 0 && !contactBlocked,
  });
  const dataQuery = useInfiniteQuery({
    queryKey: ['contacts', 'content', id, selectedTab],
    initialPageParam: undefined as string | undefined,
    enabled: !contactBlocked && (selectedTab === 'photos' || selectedTab === 'favorites'),
    queryFn: ({ pageParam, signal }) =>
      selectedTab === 'favorites'
        ? apiContactPage<ContactFavoritePage>(
            listMeContactFavorites(id, { cursor: pageParam, limit: PAGE_SIZE }, { signal }),
            id,
            setAccessFailure,
          )
        : apiContactPage<ContactPhotoPage>(
            listMeContactPhotos(id, { cursor: pageParam, limit: PAGE_SIZE }, { signal }),
            id,
            setAccessFailure,
          ),
    getNextPageParam: (page) => page.next_cursor ?? undefined,
    retry: false,
  });
  const galleries = useInfiniteQuery({
    queryKey: ['contacts', 'galleries', id],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam, signal }) =>
      apiContactPage<ContactGalleryPage>(
        listMeContactGalleries(id, { cursor: pageParam, limit: PAGE_SIZE }, { signal }),
        id,
        setAccessFailure,
      ),
    getNextPageParam: (page) => page.next_cursor ?? undefined,
    enabled: !contactBlocked && selectedTab === 'galleries',
    retry: false,
  });
  const albumsPaged = useInfiniteQuery({
    queryKey: ['contacts', 'albums', id],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam, signal }) =>
      apiContactPage<ContactAlbumPage>(
        listMeContactAlbums(id, { cursor: pageParam, limit: PAGE_SIZE }, { signal }),
        id,
        setAccessFailure,
      ),
    getNextPageParam: (page) => page.next_cursor ?? undefined,
    enabled: !contactBlocked && selectedTab === 'albums',
    retry: false,
  });
  const groups = useInfiniteQuery({
    queryKey: ['contacts', 'groups', id],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam, signal }) =>
      apiContactPage<ContactGroupPage>(
        listMeContactGroups(id, { cursor: pageParam, limit: PAGE_SIZE }, { signal }),
        id,
        setAccessFailure,
      ),
    getNextPageParam: (page) => page.next_cursor ?? undefined,
    enabled: !contactBlocked && selectedTab === 'groups',
    retry: false,
  });
  const about = useQuery({
    queryKey: ['contacts', 'about', id],
    queryFn: ({ signal }) =>
      apiContactPage<ContactAbout>(getMeContactAbout(id, { signal }), id, setAccessFailure),
    enabled: !contactBlocked && selectedTab === 'about',
    retry: false,
  });

  useEffect(() => {
    if (!contactBlocked) return;
    queryClient.removeQueries({
      predicate: (query) => query.queryKey[0] === 'contacts' && query.queryKey.includes(id),
    });
  }, [contactBlocked, id, queryClient]);

  useEffect(() => {
    const name = detail.data?.real_name || detail.data?.username || t('contact.title');
    document.title = `${name} · XFlickr`;
  }, [id, detail.data, t]);

  const star = useMutation({
    mutationFn: async (starred: boolean) => {
      const response = await starMyContact(id, { starred });
      if (response.status !== 204) throw apiError(response.status);
      return starred;
    },
    onMutate: (starred) => {
      setActionError(false);
      setActionMessage('');
      queryClient.setQueryData<ContactDetail>(personKey, (current) =>
        current ? { ...current, starred } : current,
      );
    },
    onError: () => {
      setActionError(true);
      void queryClient.invalidateQueries({ queryKey: personKey });
    },
    onSuccess: (starred) =>
      setActionMessage(t(starred ? 'contact.action.starred' : 'contact.action.unstarred')),
  });
  const toggleCrawl = useMutation({
    mutationFn: async (paused: boolean) => {
      const response = paused
        ? await resumeCrawlContact({ nsid: id })
        : await pauseCrawlContact({ nsid: id });
      if (response.status !== 204) throw apiError(response.status);
      return paused;
    },
    onMutate: (paused) => {
      setActionError(false);
      setActionMessage('');
      queryClient.setQueryData<ContactDetail>(personKey, (current) =>
        current ? { ...current, paused } : current,
      );
    },
    onError: () => {
      setActionError(true);
      void queryClient.invalidateQueries({ queryKey: personKey });
    },
    onSuccess: (paused) =>
      setActionMessage(
        t(paused ? 'contact.action.resume.success' : 'contact.action.pause.success'),
      ),
  });
  const prioritiseSizes = useMutation({
    mutationFn: async () => {
      const response = await prioritiseMeContactSizes(id);
      if (response.status !== 200) throw apiError(response.status);
      return response.data.prioritised;
    },
    onMutate: () => {
      setActionError(false);
      setActionMessage('');
    },
    onSuccess: (count) => {
      setActionError(false);
      setActionMessage(t('contact.action.prioritise-sizes.success', { count }));
      void queryClient.invalidateQueries({ queryKey: personKey });
    },
    onError: () => setActionError(true),
  });
  const prioritize = useMutation({
    mutationFn: async () => {
      const response = await selectMyContact(id);
      if (response.status !== 204) throw apiError(response.status);
    },
    onMutate: () => {
      setActionError(false);
      setActionMessage('');
    },
    onSuccess: () => {
      setActionError(false);
      setActionMessage(t('contact.action.prioritise-contact.success'));
      void queryClient.invalidateQueries({ queryKey: personKey });
    },
    onError: () => setActionError(true),
  });
  const poolCrawl = useMutation({
    mutationFn: async ({ groupNSID, enabled }: { groupNSID: string; enabled: boolean }) => {
      const response = await setMeContactGroupPoolCrawl(id, groupNSID, { enabled });
      if (response.status !== 200) throw apiError(response.status);
      return { groupNSID, enabled };
    },
    onMutate: ({ groupNSID, enabled }) => {
      setActionError(false);
      setActionMessage('');
      queryClient.setQueryData<InfiniteData<ContactGroupPage>>(
        ['contacts', 'groups', id],
        (current) =>
          current
            ? {
                ...current,
                pages: current.pages.map((page) => ({
                  ...page,
                  items: page.items.map((item) =>
                    item.nsid === groupNSID ? { ...item, pool_crawl_enabled: enabled } : item,
                  ),
                })),
              }
            : current,
      );
    },
    onError: () => {
      setActionError(true);
      setActionMessage(t('contact.groups.update-error'));
      void queryClient.invalidateQueries({ queryKey: ['contacts', 'groups', id] });
    },
  });

  const retryCurrent = () => {
    if (selectedTab === 'photos' || selectedTab === 'favorites') void dataQuery.refetch();
    else if (selectedTab === 'albums') void albumsPaged.refetch();
    else if (selectedTab === 'galleries') void galleries.refetch();
    else if (selectedTab === 'groups') void groups.refetch();
    else void about.refetch();
  };

  if (contactBlocked) {
    const forbidden = accessFailure?.status === 403;
    return (
      <div
        className={styles.page}
        data-testid={forbidden ? 'user-contact-photos-forbidden' : 'user-contact-not-found'}
      >
        <h1 tabIndex={-1} data-testid="user-contact-title">
          {t(forbidden ? 'contact.forbidden.title' : 'contact.not-found.title')}
        </h1>
        <p>{t(forbidden ? 'contact.forbidden.text' : 'contact.not-found.text')}</p>
        <Link className={styles.button} to="/contacts">
          {t('contact.back')}
        </Link>
      </div>
    );
  }

  if (detail.isPending) {
    return (
      <div className={styles.page} aria-busy="true" data-testid="user-contact-photos-loading">
        <p role="status">{t('contact.loading')}</p>
        <div className={styles.skeleton} aria-hidden="true" />
      </div>
    );
  }

  if (detail.isError || !detail.data) {
    const status = errorStatus(detail.error);
    const forbidden = status === 403;
    const notFound = status === 404;
    return (
      <div
        className={styles.page}
        data-testid={
          forbidden
            ? 'user-contact-photos-forbidden'
            : notFound
              ? 'user-contact-not-found'
              : 'user-contact-photos-error'
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
        {!forbidden && !notFound && (
          <button
            className={styles.button}
            onClick={() => void detail.refetch()}
            data-testid="user-contact-photos-retry"
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

  const contact = detail.data;
  const name = contact.real_name || contact.username || contact.nsid;
  const tabCount = (tab: Tab): number | null => {
    const key = tab === 'photos' ? 'photos' : tab;
    return key === 'about' ? null : contact.counts[key];
  };
  const contentError =
    selectedTab === 'photos' || selectedTab === 'favorites'
      ? dataQuery.isError && !dataQuery.data
      : selectedTab === 'albums'
        ? albumsPaged.isError && !albumsPaged.data
        : selectedTab === 'galleries'
          ? galleries.isError && !galleries.data
          : selectedTab === 'groups'
            ? groups.isError && !groups.data
            : about.isError && !about.data;
  const contentRefreshError =
    selectedTab === 'photos' || selectedTab === 'favorites'
      ? dataQuery.isError && Boolean(dataQuery.data) && !dataQuery.isFetchNextPageError
      : selectedTab === 'albums'
        ? albumsPaged.isError && Boolean(albumsPaged.data)
        : selectedTab === 'galleries'
          ? galleries.isError && Boolean(galleries.data)
          : selectedTab === 'groups'
            ? groups.isError && Boolean(groups.data)
            : about.isError && Boolean(about.data);
  const contentPending =
    selectedTab === 'photos' || selectedTab === 'favorites'
      ? dataQuery.isPending
      : selectedTab === 'albums'
        ? albumsPaged.isPending
        : selectedTab === 'galleries'
          ? galleries.isPending
          : selectedTab === 'groups'
            ? groups.isPending
            : about.isPending;
  const contentItems = dataQuery.data?.pages.flatMap((page) => page.items) ?? [];
  const pageTotal = dataQuery.data?.pages[0]?.total ?? 0;
  const albumItems = albumsPaged.data?.pages.flatMap((page) => page.items) ?? [];
  const galleryItems = galleries.data?.pages.flatMap((page) => page.items) ?? [];
  const groupItems = groups.data?.pages.flatMap((page) => page.items) ?? [];
  const photosTab = selectedTab === 'photos';

  return (
    <div className={styles.page} data-testid="user-contact-photos-page">
      <Link className={styles.breadcrumb} to="/contacts">
        {t('contact.breadcrumb')}
      </Link>
      <header className={styles.header} data-testid="user-contact-photos-header">
        {contact.cover_photo_url ? (
          <img
            className={styles.cover}
            src={contact.cover_photo_url}
            alt=""
            onError={(event) => event.currentTarget.remove()}
          />
        ) : (
          <div className={styles.coverFallback} aria-hidden="true" />
        )}
        <span className={styles.avatarWrap}>
          {contact.avatar_url && (
            <img
              className={styles.avatar}
              src={contact.avatar_url}
              alt=""
              data-testid="user-contact-photos-avatar"
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
          <p>@{contact.username || contact.nsid}</p>
          <p>
            <span>{contact.location || t('contact.value.unavailable')}</span>
            <span className={styles.identityDepth}>
              {t('contact.identity.depth', { depth: contact.depth })}
            </span>
          </p>
          {contact.found_via.map((source) => (
            <p key={source.nsid}>{t('contact.identity.found-via', { source: source.label })}</p>
          ))}
        </div>
        <div className={styles.actions}>
          <button
            className={styles.button}
            type="button"
            aria-pressed={contact.starred}
            disabled={star.isPending}
            onClick={() => star.mutate(!contact.starred)}
            data-testid="user-contact-photos-star"
          >
            {t(contact.starred ? 'contact.action.unstar' : 'contact.action.star')}
          </button>
          <button
            className={styles.button}
            type="button"
            disabled={prioritiseSizes.isPending}
            onClick={() => prioritiseSizes.mutate()}
            data-testid="user-contact-photos-prioritise-sizes"
          >
            {t('contact.action.prioritise-sizes')}
          </button>
          <button
            className={styles.button}
            type="button"
            disabled={prioritize.isPending}
            onClick={() => prioritize.mutate()}
            data-testid="user-contact-photos-prioritise-contact"
          >
            {t('contact.action.prioritise-contact')}
          </button>
          <button
            className={styles.button}
            type="button"
            disabled={toggleCrawl.isPending}
            onClick={() => toggleCrawl.mutate(contact.paused)}
            data-testid="user-contact-photos-crawl-toggle"
          >
            {t(contact.paused ? 'contact.action.resume' : 'contact.action.pause')}
          </button>
          <button
            className={styles.button}
            type="button"
            onClick={(event) => {
              downloadOpenerRef.current = event.currentTarget;
              setDownloadScope('contact');
            }}
            data-testid="user-contact-photos-download-all"
          >
            {t('contact.action.download-all')}
          </button>
        </div>
      </header>

      {(actionMessage || actionError) && (
        <p
          className={actionError ? styles.actionError : styles.actionSuccess}
          role={actionError ? 'alert' : 'status'}
        >
          {actionError ? actionMessage || t('contact.action.error') : actionMessage}
        </p>
      )}

      <section
        className={styles.crawl}
        aria-labelledby="user-contact-crawl-title"
        data-testid="user-contact-photos-crawl"
      >
        <div className={styles.sectionHeading}>
          <h2 id="user-contact-crawl-title">{t('contact.crawl.title')}</h2>
          <span className={styles.status}>
            {statusName(contact.paused ? 'paused' : contact.crawl_status, t)}
          </span>
        </div>
        <p className={styles.crawlCounts}>
          {t('contact.crawl.photos', {
            done: new Intl.NumberFormat(locale).format(contact.photo_records_crawled),
            total:
              contact.photo_records_total === null
                ? t('contact.value.unavailable')
                : new Intl.NumberFormat(locale).format(contact.photo_records_total),
          })}
          {' · '}
          {t('contact.crawl.sizes', { percent: contact.sizes_completed })}
        </p>
        <ContactFacets contact={contact} t={t} />
      </section>

      <nav
        className={styles.tabs}
        aria-label={t('contact.tabs.label')}
        data-testid="user-contact-photos-tabs"
      >
        {tabs.map((tab) => (
          <Link
            key={tab}
            to={`/contacts/${encodeURIComponent(id)}${tab === 'photos' ? '' : `?tab=${tab}`}`}
            aria-current={selectedTab === tab ? 'page' : undefined}
            className={selectedTab === tab ? styles.activeTab : styles.tab}
            data-testid={`user-contact-tab-${tab}`}
          >
            {t(`contact.tabs.${tab}` as const)}
            {tabCount(tab) !== null &&
              ` (${new Intl.NumberFormat(locale).format(tabCount(tab) ?? 0)})`}
          </Link>
        ))}
      </nav>

      {contentPending ? (
        <section
          className={styles.tabContent}
          aria-busy="true"
          data-testid={`user-contact-${selectedTab}-loading`}
        >
          <p role="status">
            {t(
              selectedTab === 'photos' || selectedTab === 'favorites'
                ? 'contact.loading'
                : 'contact.tab.loading',
            )}
          </p>
          <div className={styles.contentSkeleton} aria-hidden="true" />
        </section>
      ) : contentError ? (
        <section
          className={styles.tabContent}
          role="alert"
          data-testid={`user-contact-${selectedTab}-error`}
        >
          <h2>
            {t('contact.tab.error.title', { tab: t(`contact.tabs.${selectedTab}` as const) })}
          </h2>
          <p>{t('contact.error.text')}</p>
          <button
            className={styles.button}
            onClick={retryCurrent}
            data-testid={`user-contact-${selectedTab}-retry`}
          >
            {t('contact.error.retry')}
          </button>
        </section>
      ) : selectedTab === 'photos' || selectedTab === 'favorites' ? (
        <section className={styles.tabContent}>
          {selectedTab === 'favorites' && (
            <button
              className={styles.button}
              type="button"
              onClick={(event) => {
                downloadOpenerRef.current = event.currentTarget;
                setDownloadScope('favorites');
              }}
              data-testid="user-contact-favorites-download-all"
            >
              {t('contact.action.download-favorites')}
            </button>
          )}
          {contentItems.length === 0 ? (
            <div className={styles.empty} data-testid={`user-contact-${selectedTab}-empty`}>
              <h2>
                {t(
                  selectedTab === 'photos'
                    ? 'contact.empty.photos.title'
                    : 'contact.empty.favorites.title',
                )}
              </h2>
              <p>
                {t(
                  selectedTab === 'photos'
                    ? 'contact.empty.photos.text'
                    : 'contact.empty.favorites.text',
                )}
              </p>
              <Link className={styles.button} to="/contacts">
                {t('contact.empty.browse')}
              </Link>
            </div>
          ) : (
            <>
              <p className={styles.srOnly} role="status">
                {t('contact.grid.count', { count: pageTotal, contact_name: name })}
              </p>
              <ul
                className={styles.photoGrid}
                aria-label={t('contact.grid.label', { contact_name: name })}
                data-testid={photosTab ? 'user-contact-photos-grid' : 'user-contact-favorites-grid'}
              >
                {contentItems.map((photo) => (
                  <li
                    className={styles.photoCard}
                    key={photo.id}
                    data-testid={`user-contact-${selectedTab}-photo-${photo.id}`}
                  >
                    <button
                      className={styles.photoButton}
                      type="button"
                      onClick={(event) => {
                        lightboxOpenerRef.current = event.currentTarget;
                        setLightboxPhoto({ title: photo.title, url: photo.thumbnail_url });
                      }}
                    >
                      {photo.thumbnail_url ? (
                        <img
                          src={photo.thumbnail_url}
                          alt={photo.title}
                          onError={(event) => event.currentTarget.remove()}
                        />
                      ) : (
                        <span className={styles.photoPlaceholder} aria-hidden="true" />
                      )}
                      <span>{photo.title || t('contact.value.unavailable')}</span>
                    </button>
                    {'owner_nsid' in photo && typeof photo.owner_nsid === 'string' && (
                      <Link
                        className={styles.ownerLink}
                        to={`/contacts/${encodeURIComponent(photo.owner_nsid)}`}
                        data-testid={`user-contact-favorites-owner-${photo.id}`}
                      >
                        {'owner_name' in photo && typeof photo.owner_name === 'string'
                          ? photo.owner_name
                          : photo.owner_nsid}
                      </Link>
                    )}
                  </li>
                ))}
              </ul>
              {dataQuery.isFetchNextPageError && (
                <p role="alert" className={styles.actionError}>
                  {t('contact.more.error')}{' '}
                  <button
                    className={styles.linkButton}
                    onClick={() => void dataQuery.fetchNextPage()}
                  >
                    {t('contact.error.retry')}
                  </button>
                </p>
              )}
              {dataQuery.hasNextPage && (
                <button
                  className={styles.button}
                  disabled={dataQuery.isFetchingNextPage}
                  onClick={() => void dataQuery.fetchNextPage()}
                  data-testid={`user-contact-${selectedTab}-load-more`}
                >
                  {t(dataQuery.isFetchingNextPage ? 'contact.loading.more' : 'contact.load-more')}
                </button>
              )}
            </>
          )}
        </section>
      ) : selectedTab === 'albums' ? (
        <section className={styles.tabContent}>
          {albumItems.length ? (
            <ul className={styles.contentGrid} data-testid="user-contact-albums-grid">
              {albumItems.map((album) => (
                <li
                  key={album.id}
                  className={styles.albumCard}
                  data-testid={`user-contact-albums-album-${album.id}`}
                >
                  <div>
                    {album.cover_photo_urls[0] && (
                      <img
                        src={album.cover_photo_urls[0]}
                        alt=""
                        onError={(event) => event.currentTarget.remove()}
                      />
                    )}
                    <strong>{album.title}</strong>
                    <span>
                      {t('contact.album.count', {
                        stored: album.stored_count,
                        total: album.total_count,
                      })}
                    </span>
                  </div>
                </li>
              ))}
            </ul>
          ) : (
            <div className={styles.empty} data-testid="user-contact-albums-empty">
              <h2>{t('contact.empty.albums.title')}</h2>
              <p>{t('contact.empty.albums.text')}</p>
              <Link className={styles.button} to="/contacts">
                {t('contact.empty.browse')}
              </Link>
            </div>
          )}
          {albumsPaged.hasNextPage && (
            <button
              className={styles.button}
              disabled={albumsPaged.isFetchingNextPage}
              onClick={() => void albumsPaged.fetchNextPage()}
              data-testid="user-contact-albums-load-more"
            >
              {t(albumsPaged.isFetchingNextPage ? 'contact.loading.more' : 'contact.load-more')}
            </button>
          )}
        </section>
      ) : selectedTab === 'galleries' ? (
        <section className={styles.tabContent}>
          {galleryItems.length ? (
            <ul className={styles.contentGrid} data-testid="user-contact-galleries-grid">
              {galleryItems.map((gallery) => (
                <li
                  key={gallery.id}
                  className={styles.galleryCard}
                  data-testid={`user-contact-galleries-gallery-${gallery.id}`}
                >
                  <strong>{gallery.title}</strong>
                  <span>{t('contact.gallery.count', { count: gallery.photo_count })}</span>
                </li>
              ))}
            </ul>
          ) : (
            <div className={styles.empty} data-testid="user-contact-galleries-empty">
              <h2>{t('contact.empty.galleries.title')}</h2>
              <p>{t('contact.empty.galleries.text')}</p>
              <Link className={styles.button} to="/contacts">
                {t('contact.empty.browse')}
              </Link>
            </div>
          )}
          {galleries.hasNextPage && (
            <button
              className={styles.button}
              disabled={galleries.isFetchingNextPage}
              onClick={() => void galleries.fetchNextPage()}
              data-testid="user-contact-galleries-load-more"
            >
              {t(galleries.isFetchingNextPage ? 'contact.loading.more' : 'contact.load-more')}
            </button>
          )}
        </section>
      ) : selectedTab === 'groups' ? (
        <section className={styles.tabContent}>
          {groupItems.length ? (
            <div className={styles.tableWrap}>
              <table className={styles.groupTable} data-testid="user-contact-groups-table">
                <thead>
                  <tr>
                    <th>{t('contact.groups.name')}</th>
                    <th>{t('contact.groups.members')}</th>
                    <th>{t('contact.groups.pool')}</th>
                  </tr>
                </thead>
                <tbody>
                  {groupItems.map((group) => (
                    <tr key={group.nsid}>
                      <th scope="row">{group.name}</th>
                      <td>{new Intl.NumberFormat(locale).format(group.members_count)}</td>
                      <td>
                        <label className={styles.switchLabel}>
                          <input
                            type="checkbox"
                            aria-label={t('contact.groups.toggle', { group_name: group.name })}
                            checked={group.pool_crawl_enabled}
                            disabled={poolCrawl.isPending}
                            data-testid={`user-contact-groups-about-group-toggle-${group.nsid}`}
                            onChange={(event) =>
                              poolCrawl.mutate({
                                groupNSID: group.nsid,
                                enabled: event.currentTarget.checked,
                              })
                            }
                          />
                          <span>
                            {t(
                              group.pool_crawl_enabled
                                ? 'contact.groups.enabled'
                                : 'contact.groups.disabled',
                            )}
                          </span>
                        </label>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : (
            <div className={styles.empty} data-testid="user-contact-groups-empty">
              <h2>{t('contact.empty.groups.title')}</h2>
              <p>{t('contact.empty.groups.text')}</p>
              <Link className={styles.button} to="/contacts">
                {t('contact.empty.browse')}
              </Link>
            </div>
          )}
          {groups.hasNextPage && (
            <button
              className={styles.button}
              disabled={groups.isFetchingNextPage}
              onClick={() => void groups.fetchNextPage()}
              data-testid="user-contact-groups-load-more"
            >
              {t(groups.isFetchingNextPage ? 'contact.loading.more' : 'contact.load-more')}
            </button>
          )}
        </section>
      ) : (
        <section className={styles.tabContent}>
          <dl className={styles.aboutList}>
            <dt>{t('contact.about.nsid')}</dt>
            <dd>{contact.nsid}</dd>
            <dt>{t('contact.about.location')}</dt>
            <dd>{contact.location || t('contact.value.unavailable')}</dd>
            <dt>{t('contact.about.found-via')}</dt>
            <dd>
              {contact.found_via.map((source) => source.label).join(' → ') ||
                t('contact.value.unavailable')}
            </dd>
            <dt>{t('contact.about.depth')}</dt>
            <dd>{contact.depth}</dd>
            <dt>{t('contact.about.last-check')}</dt>
            <dd>{about.data?.last_change_check_at || t('contact.value.unavailable')}</dd>
          </dl>
          <h2>{t('contact.crawl.facets')}</h2>
          <ContactFacets
            contact={{ ...contact, facets: about.data?.facets ?? contact.facets }}
            t={t}
          />
          <div className={styles.tombstones}>
            <strong>{t('contact.about.tombstones')}</strong>
            <span>{new Intl.NumberFormat(locale).format(about.data?.tombstones.count ?? 0)}</span>
            <time>{about.data?.tombstones.updated_at || t('contact.value.unavailable')}</time>
          </div>
        </section>
      )}

      {contentRefreshError && (
        <p
          className={styles.partial}
          role="alert"
          data-testid={`user-contact-${selectedTab}-refresh-error`}
        >
          {t('contact.partial.title')}{' '}
          <button className={styles.linkButton} type="button" onClick={retryCurrent}>
            {t('contact.error.retry')}
          </button>
        </p>
      )}

      {lightboxPhoto && (
        <div
          className={styles.overlay}
          role="presentation"
          onMouseDown={(event) => {
            if (event.target === event.currentTarget) setLightboxPhoto(null);
          }}
        >
          <section
            ref={lightboxRef}
            className={styles.lightbox}
            role="dialog"
            aria-modal="true"
            aria-label={lightboxPhoto.title}
            data-testid="user-contact-photo-lightbox"
          >
            <button
              ref={lightboxCloseRef}
              className={styles.dialogClose}
              type="button"
              onClick={() => setLightboxPhoto(null)}
              aria-label={t('contact.lightbox.close')}
            />
            {lightboxPhoto.url && <img src={lightboxPhoto.url} alt={lightboxPhoto.title} />}
            <p>{lightboxPhoto.title}</p>
          </section>
        </div>
      )}
      {downloadScope && (
        <DownloadDialog
          scope={downloadScope}
          nsid={id}
          onClose={() => setDownloadScope(null)}
          onDone={(batchCount) =>
            setActionMessage(
              t(
                batchCount === 1
                  ? 'contact.download.success.toast'
                  : 'contact.download.success.batches.toast',
                { count: batchCount },
              ),
            )
          }
          onAccessFailure={setAccessFailure}
          returnFocus={() => downloadOpenerRef.current}
          t={t}
        />
      )}
    </div>
  );
}
