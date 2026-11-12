import { useQuery } from '@tanstack/react-query';
import { useEffect, useId, useLayoutEffect, useRef, useState } from 'react';
import type { JSX } from 'react';
import { getPhoto } from '../../shared/api/gen/catalog/catalog';
import type { CatalogPhoto } from '../../shared/api/gen/models';
import { Icon, useI18n } from '../../shared';
import styles from './PhotoViewer.module.css';

const FOCUSABLE =
  'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

export type PhotoViewerProps = {
  photoIds: number[];
  initialIndex: number;
  opener: HTMLElement | null;
  onClose: () => void;
};

function makeBackgroundInert(dialog: HTMLElement): () => void {
  const previous = new Map<HTMLElement, boolean>();
  let branch: HTMLElement = dialog;

  while (branch.parentElement) {
    const parent = branch.parentElement;
    for (const child of Array.from(parent.children)) {
      if (child instanceof HTMLElement && child !== branch) {
        previous.set(child, child.inert);
        child.inert = true;
      }
    }
    if (parent === document.body) break;
    branch = parent;
  }

  return () => {
    for (const [element, wasInert] of previous) {
      element.inert = wasInert;
    }
  };
}

function isTypingTarget(target: EventTarget | null): boolean {
  return (
    target instanceof HTMLElement &&
    (target.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(target.tagName))
  );
}

function photoUrl(photo: CatalogPhoto | null): string | null {
  if (!photo) return null;

  const confirmed = photo.sizes_status === 'confirmed';
  const size = photo.sizes.find((candidate) => {
    const url = confirmed ? candidate.source : candidate.url;
    return (
      typeof url === 'string' &&
      url.trim() !== '' &&
      (candidate.media === undefined || candidate.media === 'photo')
    );
  });

  if (!size) return null;
  return confirmed ? (size.source ?? null) : (size.url ?? null);
}

/** Shows one photo detail at a time over the invoking gallery. */
export function PhotoViewer({
  photoIds,
  initialIndex,
  opener,
  onClose,
}: PhotoViewerProps): JSX.Element {
  const { t } = useI18n();
  const dialog = useRef<HTMLDivElement>(null);
  const closeButton = useRef<HTMLButtonElement>(null);
  const onCloseRef = useRef(onClose);
  const photoIdsRef = useRef(photoIds);
  const instanceId = useId();
  const validInitialIndex =
    Number.isInteger(initialIndex) && initialIndex >= 0 && initialIndex < photoIds.length;
  const [index, setIndex] = useState(initialIndex);
  const [failedPreview, setFailedPreview] = useState<{ id: number; url: string } | null>(null);
  const [previewAttempt, setPreviewAttempt] = useState<{ id: number; count: number } | null>(null);
  const validIndex =
    validInitialIndex && Number.isInteger(index) && index >= 0 && index < photoIds.length;
  const currentId = validIndex ? photoIds[index] : undefined;
  const query = useQuery({
    queryKey: ['photo-viewer', instanceId, currentId],
    queryFn: () => getPhoto(currentId as number),
    enabled: currentId !== undefined,
    retry: false,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  });

  useEffect(() => {
    onCloseRef.current = onClose;
  }, [onClose]);

  useLayoutEffect(() => {
    photoIdsRef.current = photoIds;
  }, [photoIds]);

  useLayoutEffect(() => {
    const container = dialog.current;
    if (!container) return;

    const restoreBackground = makeBackgroundInert(container);
    closeButton.current?.focus({ preventScroll: true });

    const onKeyDown = (event: KeyboardEvent) => {
      const activeElement = document.activeElement;
      const activeModal =
        activeElement instanceof HTMLElement
          ? activeElement.closest<HTMLElement>(
              '[role="dialog"][aria-modal="true"], [role="alertdialog"][aria-modal="true"]',
            )
          : null;
      if (activeModal && activeModal !== container) return;

      if (event.key === 'Escape') {
        event.preventDefault();
        event.stopPropagation();
        onCloseRef.current();
        return;
      }

      if (event.key === 'Tab') {
        const items = Array.from(container.querySelectorAll<HTMLElement>(FOCUSABLE));
        const first = items[0];
        const last = items.at(-1);
        if (!first || !last) {
          event.preventDefault();
          container.focus({ preventScroll: true });
        } else if (
          (event.shiftKey &&
            (document.activeElement === first || !container.contains(document.activeElement))) ||
          (!event.shiftKey &&
            (document.activeElement === last || !container.contains(document.activeElement)))
        ) {
          event.preventDefault();
          (event.shiftKey ? last : first).focus({ preventScroll: true });
        }
        return;
      }

      if (!container.contains(activeElement)) return;
      const photoCount = photoIdsRef.current.length;
      if (isTypingTarget(event.target) || photoCount === 0) return;
      if (event.key === 'ArrowLeft') {
        event.preventDefault();
        setIndex((current) => (current - 1 + photoCount) % photoCount);
      } else if (event.key === 'ArrowRight') {
        event.preventDefault();
        setIndex((current) => (current + 1) % photoCount);
      }
    };

    document.addEventListener('keydown', onKeyDown);
    return () => {
      document.removeEventListener('keydown', onKeyDown);
      restoreBackground();
      const target = opener?.isConnected ? opener : document.querySelector<HTMLElement>('h1');
      if (target) {
        if (target.tagName === 'H1' && !target.hasAttribute('tabindex')) target.tabIndex = -1;
        target.focus({ preventScroll: true });
      }
    };
  }, [opener]);

  const response = query.data;
  const status = response?.status;
  const photo = response?.status === 200 ? response.data : null;
  const url = photoUrl(photo);
  const imageFailed =
    currentId !== undefined && failedPreview?.id === currentId && failedPreview.url === url;
  const retryNumber =
    currentId !== undefined && previewAttempt?.id === currentId ? previewAttempt.count : 0;
  const showEmpty = currentId === undefined;
  const showNotFound = status === 404;
  const showSessionExpired = status === 401;
  const showRequestError = query.isError || (status !== undefined && status >= 500);
  const showPreviewError = status === 200 && photo !== null && (url === null || imageFailed);
  const loading = currentId !== undefined && (query.isPending || query.isFetching);
  const heading = showEmpty
    ? t('lightbox.empty.title')
    : loading
      ? t('lightbox.title')
      : showNotFound
        ? t('lightbox.error.not-found')
        : showRequestError
          ? t('lightbox.error.title')
          : photo?.title.trim() || t('lightbox.title');

  const move = (amount: number) => {
    setIndex((current) => (current + amount + photoIds.length) % photoIds.length);
  };

  const retry = () => {
    if (currentId !== undefined && url !== null && imageFailed) {
      setFailedPreview(null);
      setPreviewAttempt({ id: currentId, count: retryNumber + 1 });
      return;
    }
    void query.refetch();
  };

  const showPrevious = () => move(-1);
  const showNext = () => move(1);
  return (
    <div
      ref={dialog}
      className={styles.dialog}
      role="dialog"
      aria-modal="true"
      aria-label={heading}
      aria-busy={loading}
      data-testid="photo-lightbox"
      tabIndex={-1}
    >
      <section className={styles.stage} data-testid="lightbox-stage">
        <header className={styles.toolbar}>
          {currentId !== undefined ? (
            <div className={styles.position} data-testid="lightbox-position" aria-live="polite">
              {t('lightbox.position', { position: index + 1, count: photoIds.length })}
            </div>
          ) : (
            <span />
          )}
          <button
            ref={closeButton}
            className={styles.control}
            type="button"
            aria-label={t('lightbox.close')}
            data-testid="lightbox-close"
            onClick={() => onCloseRef.current()}
          >
            <Icon name="x" size={20} />
          </button>
        </header>

        {validIndex ? (
          <>
            <button
              className={`${styles.control} ${styles.previous}`}
              type="button"
              aria-label={t('lightbox.previous')}
              data-testid="lightbox-previous"
              onClick={showPrevious}
            >
              <Icon name="left" size={24} />
            </button>
            <button
              className={`${styles.control} ${styles.next}`}
              type="button"
              aria-label={t('lightbox.next')}
              data-testid="lightbox-next"
              onClick={showNext}
            >
              <Icon name="right" size={24} />
            </button>
          </>
        ) : null}

        <div className={styles.imageArea}>
          {showEmpty ? (
            <div className={styles.message} data-testid="lightbox-empty">
              <h2>{t('lightbox.empty.title')}</h2>
              <p>{t('lightbox.empty.body')}</p>
              <button
                className={styles.action}
                type="button"
                data-testid="lightbox-empty-action"
                onClick={() => onCloseRef.current()}
              >
                {t('lightbox.empty.action')}
              </button>
            </div>
          ) : loading ? (
            <div
              className={styles.loading}
              data-testid="lightbox-loading"
              role="status"
              aria-busy="true"
            >
              <span className={styles.skeletonImage} aria-hidden="true" />
              <span className={styles.srOnly}>{t('lightbox.loading.label')}</span>
            </div>
          ) : showNotFound ? (
            <div className={styles.message} data-testid="lightbox-not-found" role="alert">
              <h2>{t('lightbox.error.not-found')}</h2>
              <button
                className={styles.action}
                type="button"
                data-testid="lightbox-not-found-return"
                onClick={() => onCloseRef.current()}
              >
                {t('lightbox.empty.action')}
              </button>
            </div>
          ) : showRequestError ? (
            <div className={styles.message} data-testid="lightbox-error" role="alert">
              <h2>{t('lightbox.error.title')}</h2>
              <p>{t('lightbox.error.body')}</p>
              <button
                className={styles.action}
                type="button"
                data-testid="lightbox-retry"
                onClick={retry}
              >
                {t('lightbox.retry')}
              </button>
            </div>
          ) : showPreviewError ? (
            <div className={styles.message} role="alert">
              <p data-testid="lightbox-preview-error">{t('lightbox.error.preview')}</p>
              <button
                className={styles.action}
                type="button"
                data-testid="lightbox-retry"
                onClick={retry}
              >
                {t('lightbox.retry')}
              </button>
            </div>
          ) : showSessionExpired ? (
            <div className={styles.blank} aria-hidden="true" />
          ) : url !== null && photo !== null ? (
            <img
              key={`${currentId}:${url}:${retryNumber}`}
              className={styles.image}
              src={url}
              alt={photo.title.trim() || t('lightbox.title')}
              data-testid="lightbox-image"
              onError={() => {
                if (currentId !== undefined) setFailedPreview({ id: currentId, url });
              }}
            />
          ) : null}
        </div>
      </section>

      <aside
        className={styles.panel}
        aria-hidden={showEmpty || showNotFound || showRequestError || showSessionExpired}
      >
        {loading ? (
          <div className={styles.panelSkeleton} aria-hidden="true">
            <span />
            <span />
            <span />
          </div>
        ) : null}
        {!showEmpty && !showNotFound && !showRequestError && !showSessionExpired ? (
          <h1 id="lightbox-title" className={styles.title}>
            {heading}
          </h1>
        ) : null}
      </aside>
    </div>
  );
}
