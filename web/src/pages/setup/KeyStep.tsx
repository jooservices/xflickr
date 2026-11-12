import type { ReactNode } from 'react';
import { useI18n } from '../../shared';
import { KeyActions } from './KeyActions';
import { KeyBannerView } from './KeyBannerView';
import { KeyIdField, SecretField } from './KeyFields';
import styles from './SetupPage.module.css';
import type { KeyForm } from './useKeyForm';

type KeyStepProps = {
  keyForm: KeyForm;
  rollback?: ReactNode;
};

/** Step 1 of the setup wizard: the add-key form, its banner and its field errors. */
export function KeyStep({ keyForm, rollback }: KeyStepProps) {
  const { t } = useI18n();
  return (
    <div className={styles.step} data-testid="wizard-step-key">
      <p className={styles.lead}>{t('setup.key.lead')}</p>
      <aside className={styles.callout} data-testid="wizard-key-why">
        <b>{t('setup.key.why.title')}</b>
        <p>{t('setup.key.why.text')}</p>
      </aside>
      {keyForm.banner ? (
        <KeyBannerView
          banner={keyForm.banner}
          bannerRef={keyForm.bannerRef}
          onRetry={() => void keyForm.submit()}
        />
      ) : null}
      <form
        onSubmit={(event) => void keyForm.submit(event)}
        noValidate
        data-testid="wizard-key-form"
      >
        <KeyIdField keyForm={keyForm} />
        <SecretField keyForm={keyForm} />
        <a
          href="https://www.flickr.com/services/apps/create"
          target="_blank"
          rel="noreferrer"
          data-testid="wizard-key-create-link"
        >
          {t('setup.key.create-link')}
        </a>
        {keyForm.sending ? (
          <p role="status" className={styles.note} data-testid="wizard-key-checking">
            {t('setup.key.checking')}
          </p>
        ) : null}
        <KeyActions keyForm={keyForm} rollback={rollback} />
      </form>
    </div>
  );
}
