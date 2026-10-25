import { useRef } from 'react';
import { useI18n } from '../../shared';
import { useDialog } from './useDialog';
import styles from './keys.module.css';

type RemoveKeyDialogProps = {
  label: string;
  accounts: number;
  busy: boolean;
  onConfirm: () => void;
  onCancel: () => void;
};

/** Confirm dialog for removing a key; the key's accounts go with it. */
export function RemoveKeyDialog({
  label,
  accounts,
  busy,
  onConfirm,
  onCancel,
}: RemoveKeyDialogProps) {
  const { t } = useI18n();
  const dialog = useRef<HTMLDivElement>(null);
  const cancel = useRef<HTMLButtonElement>(null);

  useDialog(dialog, onCancel, () => cancel.current?.focus());

  return (
    <div className={styles.scrim}>
      <div
        ref={dialog}
        className={styles.modal}
        role="alertdialog"
        aria-modal="true"
        aria-labelledby="keys-remove-title"
        aria-describedby="keys-remove-text"
        data-testid="keys-remove-modal"
      >
        <h2 id="keys-remove-title">{t('keys.remove.title')}</h2>
        <p id="keys-remove-text" className={styles.note}>
          {t('keys.remove.text', { label, count: accounts })}
        </p>
        <div className={styles.footer}>
          <button
            ref={cancel}
            type="button"
            className={styles.ghost}
            onClick={onCancel}
            data-testid="keys-remove-cancel"
          >
            {t('common.cancel')}
          </button>
          <button
            type="button"
            className={styles.danger}
            disabled={busy}
            onClick={onConfirm}
            data-testid="keys-remove-confirm"
          >
            {t('keys.remove.confirm')}
          </button>
        </div>
      </div>
    </div>
  );
}
