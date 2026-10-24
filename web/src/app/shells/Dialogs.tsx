import type { ReactNode } from 'react';
import { useI18n } from '../../shared';
import { useOverlay } from './useOverlay';
import styles from './shells.module.css';

type ModalProps = {
  titleId: string;
  title: string;
  role: 'dialog' | 'alertdialog';
  testId: string;
  onClose?: () => void;
  children: ReactNode;
};

function Modal({ titleId, title, role, testId, onClose, children }: ModalProps) {
  const ref = useOverlay<HTMLDivElement>({
    open: true,
    onClose: () => {
      onClose?.();
    },
    trap: true,
  });

  return (
    <div className={styles.scrimCenter}>
      <div
        ref={ref}
        className={styles.modal}
        role={role}
        aria-modal="true"
        aria-labelledby={titleId}
        tabIndex={-1}
        data-testid={testId}
      >
        <h2 id={titleId}>{title}</h2>
        {children}
      </div>
    </div>
  );
}

type SessionModalProps = { onSignIn: () => void };

/** Shown when any API call answers 401. It cannot be dismissed; only signing in continues. */
export function SessionModal({ onSignIn }: SessionModalProps) {
  const { t } = useI18n();

  return (
    <Modal
      titleId="session-title"
      title={t('shell.session.title')}
      role="alertdialog"
      testId="shell-session-modal"
    >
      <p>{t('shell.session.text')}</p>
      <div className={styles.modalActions}>
        <button
          type="button"
          className={`${styles.btn} ${styles.primary}`}
          onClick={onSignIn}
          data-testid="shell-session-sign-in"
        >
          {t('shell.session.sign-in')}
        </button>
      </div>
    </Modal>
  );
}

type ShortcutsDialogProps = { onClose: () => void };

/** Keyboard shortcut help. */
export function ShortcutsDialog({ onClose }: ShortcutsDialogProps) {
  const { t } = useI18n();
  const rows: [string, string][] = [
    ['/', t('shell.shortcuts.search')],
    ['l', t('shell.shortcuts.live')],
    ['g h', t('shell.shortcuts.home')],
    ['?', t('shell.shortcuts.help')],
  ];

  return (
    <Modal
      titleId="shortcuts-title"
      title={t('shell.shortcuts.title')}
      role="dialog"
      testId="shell-shortcuts"
      onClose={onClose}
    >
      <dl className={styles.shortcuts}>
        {rows.map(([keys, text]) => (
          <div key={keys}>
            <dt>
              <kbd>{keys}</kbd>
            </dt>
            <dd>{text}</dd>
          </div>
        ))}
      </dl>
      <div className={styles.modalActions}>
        <button type="button" className={styles.btn} onClick={onClose}>
          {t('common.close')}
        </button>
      </div>
    </Modal>
  );
}
