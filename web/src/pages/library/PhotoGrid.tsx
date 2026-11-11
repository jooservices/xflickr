import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { useWindowVirtualizer } from '@tanstack/react-virtual';
import type { CatalogFavorite, CatalogPhoto } from '../../shared/api/gen/models';
import { useI18n } from '../../shared/i18n';
import { buildJustifiedRows } from './layout';
import styles from './LibraryPage.module.css';

type PhotoVisual = { id: number; photo: CatalogPhoto; url?: string; ratio: number };

type PhotoGridProps = {
  photos: CatalogFavorite[];
  selected: ReadonlySet<number>;
  selectionMode: boolean;
  selectionFull: boolean;
  staleNotice: boolean;
  onToggleSelected: (photoId: number) => void;
};

function visualFor(photo: CatalogPhoto): PhotoVisual {
  const candidates = photo.sizes.flatMap((size) =>
    size.url !== undefined &&
    size.width !== undefined &&
    size.height !== undefined &&
    size.width > 0 &&
    size.height > 0
      ? [{ url: size.url, width: size.width, height: size.height }]
      : [],
  );
  candidates.sort((left, right) => left.width * left.height - right.width * right.height);
  const size = candidates.find((candidate) => candidate.width >= 320) ?? candidates[0];
  return {
    id: photo.id,
    photo,
    ...(size === undefined ? {} : { url: size.url }),
    ratio: size === undefined ? 4 / 3 : size.width / size.height,
  };
}

export function PhotoGrid({
  photos,
  selected,
  selectionMode,
  selectionFull,
  staleNotice,
  onToggleSelected,
}: PhotoGridProps) {
  const { locale, t } = useI18n();
  const gridRef = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(1200);
  const [scrollMargin, setScrollMargin] = useState(0);
  const visuals = useMemo(() => photos.map(({ photo }) => visualFor(photo)), [photos]);
  const rows = useMemo(() => buildJustifiedRows(visuals, width), [visuals, width]);
  const virtualizer = useWindowVirtualizer({
    count: rows.length,
    estimateSize: (index) => (rows[index]?.height ?? 190) + 12,
    getItemKey: (index) => rows[index]?.key ?? index,
    overscan: 4,
    scrollMargin,
    useFlushSync: false,
  });

  // ResizeObserver keeps natural ratios justified as the shell and filter rail reflow.
  useEffect(() => {
    const node = gridRef.current;
    if (node === null || typeof ResizeObserver === 'undefined') return undefined;
    const observer = new ResizeObserver((entries) => {
      const nextWidth = entries[0]?.contentRect.width;
      if (nextWidth !== undefined && nextWidth > 0) setWidth(nextWidth);
    });
    observer.observe(node);
    return () => observer.disconnect();
  }, []);

  const measureScrollMargin = useCallback(() => {
    const grid = gridRef.current;
    if (grid === null) return;
    const next = Math.round(grid.getBoundingClientRect().top + window.scrollY);
    setScrollMargin((current) => (current === next ? current : next));
  }, []);

  useLayoutEffect(() => {
    measureScrollMargin();
  }, [locale, measureScrollMargin, photos.length, selectionFull, selectionMode, staleNotice]);

  // Content above the grid (filters, result bar, notices) can change height without changing any prop
  // used above or the size of <body>, which would leave the virtualizer positioning rows from a stale
  // offset. Observe the grid's container and every element rendered before the grid at each level up to
  // <body>, so a size change anywhere above it re-measures the offset.
  useEffect(() => {
    window.addEventListener('resize', measureScrollMargin);
    if (typeof ResizeObserver === 'undefined') {
      return () => window.removeEventListener('resize', measureScrollMargin);
    }
    const observer = new ResizeObserver(() => measureScrollMargin());
    let node: Element | null = gridRef.current;
    while (node !== null && node !== document.body) {
      for (
        let before = node.previousElementSibling;
        before !== null;
        before = before.previousElementSibling
      ) {
        observer.observe(before);
      }
      node = node.parentElement;
      if (node !== null) observer.observe(node);
    }
    observer.observe(document.body);
    return () => {
      observer.disconnect();
      window.removeEventListener('resize', measureScrollMargin);
    };
  }, [measureScrollMargin]);

  const number = new Intl.NumberFormat(locale);
  const virtualRows = virtualizer.getVirtualItems();

  return (
    <div
      className={styles.gridViewport}
      ref={gridRef}
      data-testid="user-library-grid"
      data-scroll-margin={scrollMargin}
      role="region"
      aria-label={t('user.library.results.label')}
      aria-busy="false"
    >
      <div
        className={styles.virtualCanvas}
        style={{ height: `${String(virtualizer.getTotalSize())}px` }}
      >
        {virtualRows.map((virtualRow) => {
          const row = rows[virtualRow.index];
          if (row === undefined) return null;
          return (
            <div
              key={virtualRow.key}
              ref={virtualizer.measureElement}
              data-index={virtualRow.index}
              className={styles.virtualRow}
              style={{
                transform: `translateY(${String(virtualRow.start - scrollMargin)}px)`,
                height: `${String(row.height + 12)}px`,
              }}
            >
              <div
                className={styles.justifiedRow}
                role="list"
                style={{ height: `${String(row.height)}px` }}
              >
                {row.tiles.map(({ item }) => {
                  const photo = item.photo;
                  const isSelected = selected.has(photo.id);
                  const sizesReady = photo.sizes_status === 'confirmed';
                  const captioned = photo.ai_status === 'ready';
                  const title = photo.title.trim() || t('user.library.photo.untitled');
                  const alt = `${title} ${t('user.library.photo.by', { owner: photo.owner_nsid })}`;
                  return (
                    <article
                      key={photo.id}
                      className={styles.photoTile}
                      data-testid={`user-library-photo-${String(photo.id)}`}
                      role="listitem"
                      tabIndex={selectionMode ? undefined : 0}
                      style={{ flexBasis: `${String(item.ratio * row.height)}px` }}
                    >
                      {item.url === undefined ? (
                        <div className={styles.photoPlaceholder} role="img" aria-label={alt} />
                      ) : (
                        <img
                          className={styles.photoImage}
                          src={item.url}
                          alt={alt}
                          loading="lazy"
                        />
                      )}
                      <div className={styles.flags} aria-label={t('user.library.photo.statuses')}>
                        <span
                          className={sizesReady ? styles.readyFlag : styles.pendingFlag}
                          data-testid={`user-library-photo-sizes-${String(photo.id)}`}
                        >
                          {sizesReady
                            ? t('user.library.photo.sizes-ready')
                            : t('user.library.photo.sizes-pending')}
                        </span>
                        <span
                          className={captioned ? styles.readyFlag : styles.pendingFlag}
                          data-testid={`user-library-photo-captioned-${String(photo.id)}`}
                        >
                          {captioned
                            ? t('user.library.photo.captioned')
                            : t('user.library.photo.caption-pending')}
                        </span>
                      </div>
                      <div
                        className={styles.photoCaption}
                        data-testid={`user-library-photo-caption-${String(photo.id)}`}
                      >
                        <span>{title}</span>
                        <small>{photo.owner_nsid}</small>
                      </div>
                      {selectionMode ? (
                        <div className={styles.photoActions}>
                          <label className={styles.selectPhoto}>
                            <input
                              type="checkbox"
                              checked={isSelected}
                              disabled={selectionFull && !isSelected}
                              aria-label={t('user.library.photo.select', { title })}
                              data-testid={`user-library-photo-select-${String(photo.id)}`}
                              onChange={() => onToggleSelected(photo.id)}
                            />
                          </label>
                        </div>
                      ) : null}
                    </article>
                  );
                })}
              </div>
            </div>
          );
        })}
      </div>
      <span className={styles.srOnly} aria-live="polite">
        {t('user.library.results.loaded', { count: number.format(photos.length) })}
      </span>
    </div>
  );
}
