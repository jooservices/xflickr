import { useRef, useState } from 'react';
import type { FormEvent, ReactNode, RefObject } from 'react';
import { Icon, useI18n } from '../../shared';
import type { MessageKey } from '../../shared/i18n';
import type { ApiKey } from '../../shared/api/gen/models';
import { clean, keyPattern, saveKey, secretPattern } from '../../features/keys/api';
import type { KeyFailure } from '../../features/keys/api';
import { useDialog } from './useDialog';
import styles from './keys.module.css';

const bannerCopy: Record<NonNullable<KeyFailure['banner']>, MessageKey> = {
  invalid: 'keys.error.key-invalid',
  network: 'keys.error.add-failed',
  node: 'keys.error.no-node',
};

type FieldProps = {
  id: string;
  label: string;
  error: ReactNode;
  errorTestId: string;
  children: ReactNode;
};

function Field({ id, label, error, errorTestId, children }: FieldProps) {
  return (
    <div className={styles.field}>
      <label htmlFor={id}>{label}</label>
      {children}
      {error ? (
        <small id={`${id}-err`} className={styles.fieldError} data-testid={errorTestId}>
          <Icon name="alert" size={14} /> {error}
        </small>
      ) : null}
    </div>
  );
}

type AddKeyDialogProps = {
  /** Called once the server accepted the key; the caller closes the dialog. */
  onAdded: (key: ApiKey) => void;
  onClose: () => void;
};

/**
 * Add API key dialog of the Keys page: label (1 to 80 characters), key (32 hex)
 * and secret (16 hex), one `POST /keys`. Nothing typed is kept after it closes.
 */
export function AddKeyDialog({ onAdded, onClose }: AddKeyDialogProps) {
  const { t } = useI18n();
  const dialog = useRef<HTMLDivElement>(null);
  const banner = useRef<HTMLDivElement>(null);
  const labelField = useRef<HTMLInputElement>(null);
  const keyField = useRef<HTMLInputElement>(null);
  const secretField = useRef<HTMLInputElement>(null);
  const sendingRef = useRef(false);

  const [label, setLabel] = useState('');
  const [key, setKey] = useState('');
  const [secret, setSecret] = useState('');
  const [showSecret, setShowSecret] = useState(false);
  const [touched, setTouched] = useState({ label: false, key: false, secret: false });
  const [submitted, setSubmitted] = useState(false);
  const [failure, setFailure] = useState<KeyFailure>({});
  const [sending, setSending] = useState(false);

  useDialog(dialog, onClose, () => labelField.current?.focus());

  const labelInvalid = label.trim().length < 1 || label.trim().length > 80;
  const keyInvalid = !keyPattern.test(clean(key));
  const secretInvalid = !secretPattern.test(clean(secret));
  const show = (name: keyof typeof touched) => touched[name] || submitted;
  const labelError = show('label') && labelInvalid;
  const keyError = failure.fields?.key ?? (show('key') && keyInvalid ? 'format' : null);
  const secretError = failure.fields?.secret ?? (show('secret') && secretInvalid ? 'format' : null);
  const touch = (name: keyof typeof touched) =>
    setTouched((current) => ({ ...current, [name]: true }));
  const clearField = (name: 'key' | 'secret') => {
    setFailure((current) => ({
      ...(current.banner === 'invalid' ? {} : current),
      fields: { ...current.fields, [name]: undefined },
    }));
  };

  const focusInvalid = (): RefObject<HTMLInputElement | null> | null => {
    if (labelInvalid) return labelField;
    if (keyInvalid) return keyField;
    if (secretInvalid) return secretField;
    return null;
  };

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (sendingRef.current) return;
    setSubmitted(true);
    setFailure({});
    const invalid = focusInvalid();
    if (invalid !== null) return void invalid.current?.focus();
    sendingRef.current = true;
    setSending(true);
    const result = await saveKey({ label: label.trim(), key: clean(key), secret: clean(secret) });
    sendingRef.current = false;
    setSending(false);
    if ('key' in result) {
      setKey('');
      setSecret('');
      setShowSecret(false);
      onAdded(result.key);
      return;
    }
    setFailure(result.failure);
    if (result.failure.banner) window.setTimeout(() => banner.current?.focus(), 0);
    else if (result.failure.fields?.key) keyField.current?.focus();
    else if (result.failure.fields?.secret) secretField.current?.focus();
  };

  return (
    <div className={styles.scrim}>
      <div
        ref={dialog}
        className={styles.modal}
        role="dialog"
        aria-modal="true"
        aria-labelledby="keys-add-title"
        data-testid="keys-add-modal"
      >
        <form onSubmit={(event) => void submit(event)} noValidate>
          <h2 id="keys-add-title">{t('keys.add.title')}</h2>
          <p className={styles.note}>{t('keys.add.help')}</p>
          {failure.banner ? (
            <div
              ref={banner}
              tabIndex={-1}
              role="alert"
              className={styles.alert}
              data-testid="keys-add-error"
            >
              {t(bannerCopy[failure.banner])}
            </div>
          ) : null}
          <Field
            id="keys-add-label"
            label={t('keys.add.label')}
            error={labelError ? t('keys.error.label-required') : null}
            errorTestId="keys-add-error-label"
          >
            <input
              id="keys-add-label"
              ref={labelField}
              value={label}
              placeholder={t('keys.add.label-placeholder')}
              onChange={(event) => setLabel(event.target.value)}
              onBlur={() => touch('label')}
              aria-invalid={labelError}
              aria-describedby={labelError ? 'keys-add-label-err' : undefined}
              autoComplete="off"
              data-testid="keys-add-label"
            />
          </Field>
          <Field
            id="keys-add-key"
            label={t('keys.add.key')}
            error={
              keyError
                ? t(keyError === 'duplicate' ? 'keys.error.key-duplicate' : 'keys.error.key-format')
                : null
            }
            errorTestId="keys-add-error-key"
          >
            <input
              id="keys-add-key"
              ref={keyField}
              className={styles.mono}
              value={key}
              placeholder={t('keys.add.key-placeholder')}
              onChange={(event) => {
                setKey(event.target.value);
                clearField('key');
              }}
              onBlur={() => touch('key')}
              aria-invalid={keyError !== null || failure.banner === 'invalid'}
              aria-describedby={keyError ? 'keys-add-key-err' : undefined}
              autoComplete="off"
              spellCheck={false}
              data-testid="keys-add-key"
            />
          </Field>
          <Field
            id="keys-add-secret"
            label={t('keys.add.secret')}
            error={secretError ? t('keys.error.secret-format') : null}
            errorTestId="keys-add-error-secret"
          >
            <span className={styles.secret}>
              <input
                id="keys-add-secret"
                ref={secretField}
                type={showSecret ? 'text' : 'password'}
                className={styles.mono}
                value={secret}
                placeholder={t('keys.add.secret-placeholder')}
                onChange={(event) => {
                  setSecret(event.target.value);
                  clearField('secret');
                }}
                onBlur={() => touch('secret')}
                aria-invalid={secretError !== null}
                aria-describedby={secretError ? 'keys-add-secret-err' : undefined}
                autoComplete="new-password"
                spellCheck={false}
                data-testid="keys-add-secret"
              />
              <button
                type="button"
                className={styles.iconButton}
                aria-label={t(showSecret ? 'keys.add.secret-hide' : 'keys.add.secret-show')}
                aria-pressed={showSecret}
                onClick={() => setShowSecret(!showSecret)}
                data-testid="keys-add-secret-toggle"
              >
                <Icon name="eye" />
              </button>
            </span>
          </Field>
          <div className={styles.footer}>
            <button
              type="button"
              className={styles.ghost}
              onClick={onClose}
              data-testid="keys-add-cancel"
            >
              {t('common.cancel')}
            </button>
            <button
              type="submit"
              className={styles.primary}
              disabled={sending}
              data-testid="keys-add-submit"
            >
              {t(sending ? 'keys.add.submitting' : 'keys.add.submit')}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
