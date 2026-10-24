import { useQueryClient } from '@tanstack/react-query';
import { useRef, useState } from 'react';
import { EmptyState, ErrorState, Icon, LoadingState, useI18n } from '../../shared';
import { CountBadge } from './Controls';
import { sendRequest, useTray } from './data';
import { useShellRuntime } from './runtime';
import { useOverlay } from './useOverlay';
import styles from './shells.module.css';

type TrayButtonProps = { count: number | undefined; onDownload: () => void };

function formatBytes(bytes: number, locale: string): string {
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  const number = new Intl.NumberFormat(locale, { maximumFractionDigits: 1 }).format(value);
  return `${number} ${units[unit] ?? 'B'}`;
}

/** Download tray button and popover (a bottom sheet on narrow screens). */
export function Tray({ count, onDownload }: TrayButtonProps) {
  const { t, locale } = useI18n();
  const { toast } = useShellRuntime();
  const client = useQueryClient();
  const [open, setOpen] = useState(false);
  const tray = useTray(open);
  const close = () => {
    setOpen(false);
  };
  const openerRef = useRef<HTMLButtonElement>(null);
  const ref = useOverlay<HTMLDivElement>({
    open,
    onClose: close,
    closeOnOutside: true,
    openerRef,
  });
  const items = tray.data?.items ?? [];
  const refresh = () => client.invalidateQueries({ queryKey: ['shell'] });

  const remove = async (photoId: string) => {
    await sendRequest(`/tray/items/${encodeURIComponent(photoId)}`, 'DELETE');
    toast(t('shell.tray.removed-toast'), {
      label: t('shell.tray.undo'),
      run: () => {
        void sendRequest(`/tray/items/${encodeURIComponent(photoId)}`, 'PUT').then(refresh);
      },
    });
    await refresh();
  };

  const clear = async () => {
    await sendRequest('/tray', 'DELETE');
    await refresh();
  };

  let body;
  if (tray.isPending) {
    body = <LoadingState testId="shell-tray" tiles={2} />;
  } else if (tray.isError) {
    body = (
      <ErrorState
        testId="shell-tray"
        title={t('shell.tray.error')}
        text=""
        onRetry={() => {
          void tray.refetch();
        }}
      />
    );
  } else if (items.length === 0) {
    body = (
      <EmptyState
        testId="shell-tray"
        icon="tray"
        title={t('shell.tray.empty.title')}
        text={t('shell.tray.empty.text')}
      />
    );
  } else {
    body = (
      <>
        <ul className={styles.trayList}>
          {items.map((item) => (
            <li key={item.photo_id} data-testid={`shell-tray-item-${item.photo_id}`}>
              <img src={item.thumb_url} alt="" width={40} height={40} />
              <span>
                <strong>{item.title}</strong>
                <small>{item.owner_name}</small>
              </span>
              <button
                type="button"
                className={styles.iconBtn}
                aria-label={t('shell.tray.remove')}
                onClick={() => {
                  void remove(item.photo_id);
                }}
                data-testid={`shell-tray-item-${item.photo_id}-remove`}
              >
                <Icon name="x" />
              </button>
            </li>
          ))}
        </ul>
        <p className={styles.traySummary} data-testid="shell-tray-summary">
          {t('shell.tray.summary', {
            count: tray.data?.count ?? items.length,
            size: formatBytes(tray.data?.estimated_bytes ?? 0, locale),
          })}
        </p>
        <div className={styles.modalActions}>
          <button
            type="button"
            className={styles.btn}
            onClick={() => {
              void clear();
            }}
            data-testid="shell-tray-clear"
          >
            {t('shell.tray.clear')}
          </button>
          <button
            type="button"
            className={`${styles.btn} ${styles.primary}`}
            onClick={() => {
              close();
              onDownload();
            }}
            data-testid="shell-tray-download"
          >
            {t('shell.tray.download')}
          </button>
        </div>
      </>
    );
  }

  return (
    <div className={styles.anchor}>
      <button
        ref={openerRef}
        type="button"
        className={styles.iconBtn}
        aria-haspopup="dialog"
        aria-expanded={open}
        onClick={() => {
          setOpen(!open);
        }}
        data-testid="shell-tray-button"
      >
        <Icon name="tray" />
        <span className={styles.srOnly}>{t('shell.tray.button', { count: count ?? 0 })}</span>
        <CountBadge count={count} testId="shell-tray-count" />
      </button>
      {open ? (
        <div
          ref={ref}
          className={styles.popover}
          role="dialog"
          aria-label={t('shell.tray.title')}
          data-testid="shell-tray"
        >
          <h2>{t('shell.tray.title')}</h2>
          {body}
        </div>
      ) : null}
    </div>
  );
}
