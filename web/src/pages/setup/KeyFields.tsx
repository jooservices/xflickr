import { Icon, useI18n } from '../../shared';
import styles from './SetupPage.module.css';
import type { KeyForm } from './useKeyForm';

/** The Flickr key input with its hint and its format error. */
export function KeyIdField({ keyForm }: { keyForm: KeyForm }) {
  const { t } = useI18n();
  const { key, keyError, banner, keyField, setField, blurField } = keyForm;
  return (
    <>
      <label>
        {t('setup.key.field.key')}
        <input
          ref={keyField}
          className={styles.mono}
          value={key}
          onChange={(event) => setField('key', event.target.value)}
          onBlur={() => blurField('key')}
          aria-describedby={`wizard-key-help${keyError ? ' wizard-key-err' : ''}`}
          aria-invalid={keyError !== null || banner === 'invalid'}
          autoComplete="off"
          data-testid="wizard-key-input"
        />
      </label>
      <small id="wizard-key-help">{t('setup.key.help.key')}</small>
      {keyError ? (
        <small id="wizard-key-err" className={styles.fieldError} data-testid="wizard-key-error-key">
          <Icon name="alert" size={14} />{' '}
          {t(keyError === 'duplicate' ? 'setup.key.error.duplicate' : 'setup.key.error.key-format')}
        </small>
      ) : null}
    </>
  );
}

/** The secret input with its show/hide toggle, its hint and its format error. */
export function SecretField({ keyForm }: { keyForm: KeyForm }) {
  const { t } = useI18n();
  const { secret, showSecret, setShowSecret, secretError, secretField, setField, blurField } =
    keyForm;
  return (
    <>
      <label>
        {t('setup.key.field.secret')}
        <span className={styles.secret}>
          <input
            ref={secretField}
            type={showSecret ? 'text' : 'password'}
            className={styles.mono}
            value={secret}
            onChange={(event) => setField('secret', event.target.value)}
            onBlur={() => blurField('secret')}
            aria-describedby={`wizard-secret-help${secretError ? ' wizard-secret-err' : ''}`}
            aria-invalid={secretError !== null}
            autoComplete="new-password"
            data-testid="wizard-key-secret"
          />
          <button
            type="button"
            aria-label={t(showSecret ? 'setup.key.hide' : 'setup.key.show')}
            aria-pressed={showSecret}
            onClick={() => setShowSecret(!showSecret)}
            data-testid="wizard-key-secret-toggle"
          >
            <Icon name="eye" />
          </button>
        </span>
      </label>
      <small id="wizard-secret-help">{t('setup.key.help.secret')}</small>
      {secretError ? (
        <small
          id="wizard-secret-err"
          className={styles.fieldError}
          data-testid="wizard-key-error-secret"
        >
          <Icon name="alert" size={14} /> {t('setup.key.error.secret-format')}
        </small>
      ) : null}
    </>
  );
}
