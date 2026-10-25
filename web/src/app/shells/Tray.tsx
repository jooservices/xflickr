import { useRef, useState } from 'react';
import { EmptyState, Icon, useI18n } from '../../shared';
import { CountBadge } from './Controls';
import { useOverlay } from './useOverlay';
import styles from './shells.module.css';

type TrayButtonProps = { count: number | undefined; onDownload: () => void };

/** Download tray button and popover (a bottom sheet on narrow screens). */
export function Tray({ count }: TrayButtonProps) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
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
  const body = (
    <EmptyState
      testId="shell-tray"
      icon="tray"
      title={t('common.not-available.title')}
      text={t('common.not-available.text')}
    />
  );

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
