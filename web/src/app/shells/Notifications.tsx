import { useRef, useState } from 'react';
import { EmptyState, Icon, useI18n } from '../../shared';
import { CountBadge } from './Controls';
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
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const close = () => {
    setOpen(false);
  };
  const openerRef = useRef<HTMLButtonElement>(null);
  const ref = useOverlay<HTMLDivElement>({ open, onClose: close, trap: true, openerRef });

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
            <EmptyState
              testId="shell-notifications"
              icon="bell"
              title={t('common.not-available.title')}
              text={t('common.not-available.text')}
            />
          </div>
        </div>
      ) : null}
    </>
  );
}
