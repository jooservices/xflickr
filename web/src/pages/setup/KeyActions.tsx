import { useI18n } from '../../shared';
import styles from './SetupPage.module.css';
import { DEMO } from './setupModel';
import type { KeyForm } from './useKeyForm';

type KeyActionsProps = {
  keyForm: KeyForm;
  hasAccount: boolean;
  onBack: () => void;
};

/** The submit button of the add-key form, the demo fill and the way back to step 2 or 3. */
export function KeyActions({ keyForm, hasAccount, onBack }: KeyActionsProps) {
  const { t } = useI18n();
  const { sending, fillExample } = keyForm;
  return (
    <div className={styles.actions}>
      <button
        className={styles.primary}
        disabled={sending}
        type="submit"
        data-testid="wizard-key-submit"
      >
        {sending ? t('setup.key.checking') : t('setup.key.submit')}
      </button>
      {DEMO ? (
        <button
          type="button"
          className={styles.ghost}
          onClick={fillExample}
          data-testid="wizard-key-fill"
        >
          {t('setup.key.fill')}
        </button>
      ) : null}
      {hasAccount ? (
        <button
          type="button"
          className={styles.ghost}
          onClick={onBack}
          data-testid="wizard-key-back"
        >
          {t('setup.connect.back')}
        </button>
      ) : null}
    </div>
  );
}
