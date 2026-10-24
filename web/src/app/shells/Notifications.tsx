import { useQueryClient } from '@tanstack/react-query';
import { useRef, useState } from 'react';
import {
  EmptyState,
  ErrorState,
  Icon,
  LoadingState,
  catalogs,
  translate,
  useI18n,
} from '../../shared';
import type { MessageKey } from '../../shared';
import { CountBadge } from './Controls';
import { sendRequest, useNotifications } from './data';
import { useOverlay } from './useOverlay';
import styles from './shells.module.css';

type BellProps = {
  count: number | undefined;
  /** Test id of the bell button and of its count badge. */
  buttonTestId: string;
  countTestId: string;
};

/** Bell with the unread badge, and the notifications drawer it opens. */
export function Notifications({ count, buttonTestId, countTestId }: BellProps) {
  const { t, locale } = useI18n();
  const client = useQueryClient();
  const [open, setOpen] = useState(false);
  const list = useNotifications(open);
  const close = () => {
    setOpen(false);
  };
  const openerRef = useRef<HTMLButtonElement>(null);
  const ref = useOverlay<HTMLDivElement>({ open, onClose: close, trap: true, openerRef });
  const items = list.data?.items ?? [];

  const markAllRead = async () => {
    await sendRequest('/notifications/read', 'POST', { all: true });
    await client.invalidateQueries({ queryKey: ['shell'] });
    await client.invalidateQueries({ queryKey: ['admin'] });
  };

  let body;
  if (list.isPending) {
    body = <LoadingState testId="shell-notifications" tiles={2} />;
  } else if (list.isError) {
    body = (
      <ErrorState
        testId="shell-notifications"
        title={t('shell.notifications.error')}
        text=""
        onRetry={() => {
          void list.refetch();
        }}
      />
    );
  } else if (items.length === 0) {
    body = (
      <EmptyState
        testId="shell-notifications"
        icon="bell"
        title={t('shell.notifications.empty.title')}
        text={t('shell.notifications.empty.text')}
      />
    );
  } else {
    body = (
      <ul className={styles.notifList}>
        {items.map((item) => (
          <li
            key={item.id}
            className={item.read ? '' : styles.unread}
            data-testid={`shell-notifications-item-${item.id}`}
          >
            {item.text_key in catalogs.en
              ? translate(locale, item.text_key as MessageKey, item.params)
              : item.text_key}
          </li>
        ))}
      </ul>
    );
  }

  return (
    <>
      <button
        ref={openerRef}
        type="button"
        className={styles.iconBtn}
        aria-haspopup="dialog"
        aria-expanded={open}
        onClick={() => {
          setOpen(!open);
        }}
        data-testid={buttonTestId}
      >
        <Icon name="bell" />
        <span className={styles.srOnly}>
          {t('shell.notifications.button', { count: count ?? 0 })}
        </span>
        <CountBadge count={count} testId={countTestId} />
      </button>
      {open ? (
        <div className={styles.scrimSide}>
          <div
            ref={ref}
            className={styles.drawer}
            role="dialog"
            aria-modal="true"
            aria-label={t('shell.notifications.title')}
            tabIndex={-1}
            data-testid="shell-notifications"
          >
            <div className={styles.drawerHead}>
              <h2>{t('shell.notifications.title')}</h2>
              <button
                type="button"
                className={styles.iconBtn}
                aria-label={t('shell.notifications.close')}
                onClick={close}
              >
                <Icon name="x" />
              </button>
            </div>
            <button
              type="button"
              className={styles.btn}
              onClick={() => {
                void markAllRead();
              }}
              data-testid="shell-notifications-mark-all"
            >
              {t('shell.notifications.mark-all')}
            </button>
            {body}
          </div>
        </div>
      ) : null}
    </>
  );
}
