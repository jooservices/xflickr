import { useEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { Icon, useI18n } from '../../shared';
import type { MessageKey } from '../../shared/i18n';
import type { ApiKey } from '../../shared/api/gen/models';
import { clean, keyPattern, saveKey, secretPattern } from './api';
import type { KeyFailure } from './api';
import styles from './AddKeyModal.module.css';

const FOCUSABLE =
  'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])';

const bannerCopy: Record<NonNullable<KeyFailure['banner']>, MessageKey> = {
  invalid: 'user.wizard.key.error.invalid',
  network: 'user.wizard.key.error.network',
  node: 'user.wizard.key.error.no-node',
};

type AddKeyModalProps = {
  /** Called once the server accepted the key; the caller closes the modal. */
  onAdded: (key: ApiKey) => void;
  onClose: () => void;
};

/**
 * Add API key dialog, shared by the setup wizard and Keys: label (1 to 80
 * characters), key and secret, one `POST /keys`. Focus is trapped while open,
 * Esc closes it and focus returns to the control that opened it. On narrow
 * screens it is a bottom sheet with a sticky footer.
 */
export function AddKeyModal({ onAdded, onClose }: AddKeyModalProps) {
  const { t } = useI18n();
  const dialog = useRef<HTMLDivElement>(null);
  const banner = useRef<HTMLDivElement>(null);
  const labelField = useRef<HTMLInputElement>(null);
  const keyField = useRef<HTMLInputElement>(null);
  const secretField = useRef<HTMLInputElement>(null);
  const onCloseRef = useRef(onClose);
  const sendingRef = useRef(false);

  const [label, setLabel] = useState('');
  const [key, setKey] = useState('');
  const [secret, setSecret] = useState('');
  const [showSecret, setShowSecret] = useState(false);
  const [touched, setTouched] = useState({ label: false, key: false, secret: false });
  const [submitted, setSubmitted] = useState(false);
  const [failure, setFailure] = useState<KeyFailure>({});
  const [sending, setSending] = useState(false);

  useEffect(() => {
    onCloseRef.current = onClose;
  }, [onClose]);

  useEffect(() => {
    const opener = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const container = dialog.current;
    labelField.current?.focus();
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.stopPropagation();
        onCloseRef.current();
        return;
      }
      if (event.key !== 'Tab' || container === null) return;
      const items = Array.from(container.querySelectorAll<HTMLElement>(FOCUSABLE));
      const head = items[0];
      const tail = items[items.length - 1];
      if (head === undefined || tail === undefined) return;
      if (event.shiftKey && document.activeElement === head) {
        event.preventDefault();
        tail.focus();
      } else if (!event.shiftKey && document.activeElement === tail) {
        event.preventDefault();
        head.focus();
      }
    };
    document.addEventListener('keydown', onKeyDown);
    return () => {
      document.removeEventListener('keydown', onKeyDown);
      if (opener?.isConnected === true) opener.focus();
    };
  }, []);

  useEffect(() => {
    if (failure.banner) banner.current?.focus();
  }, [failure.banner]);

  const labelInvalid = label.trim().length < 1 || label.trim().length > 80;
  const keyInvalid = !keyPattern.test(clean(key));
  const secretInvalid = !secretPattern.test(clean(secret));
  const show = (name: keyof typeof touched) => touched[name] || submitted;
  const labelError = show('label') && labelInvalid;
  const keyError = failure.fields?.key ?? (show('key') && keyInvalid ? 'format' : null);
  const secretError = failure.fields?.secret ?? (show('secret') && secretInvalid ? 'format' : null);
  const touch = (name: keyof typeof touched) =>
    setTouched((current) => ({ ...current, [name]: true }));

  const submit = async (event?: FormEvent) => {
    event?.preventDefault();
    if (sendingRef.current) return;
    setSubmitted(true);
    setFailure({});
    if (labelInvalid) return void labelField.current?.focus();
    if (keyInvalid) return void keyField.current?.focus();
    if (secretInvalid) return void secretField.current?.focus();
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
    if (result.failure.fields?.key) keyField.current?.focus();
    else if (result.failure.fields?.secret) secretField.current?.focus();
  };

  return (
    <div className={styles.scrim}>
      <div
        ref={dialog}
        className={styles.modal}
        role="dialog"
        aria-modal="true"
        aria-labelledby="wizard-addkey-title"
        data-testid="wizard-addkey-modal"
      >
        <form onSubmit={(event) => void submit(event)} noValidate>
          <h2 id="wizard-addkey-title">{t('user.wizard.addkey.title')}</h2>
          <p className={styles.note}>{t('user.wizard.addkey.note')}</p>
          {failure.banner ? (
            <div
              ref={banner}
              tabIndex={-1}
              role="alert"
              className={styles.error}
              data-testid="wizard-addkey-error"
            >
              {t(bannerCopy[failure.banner])}
            </div>
          ) : null}
          <label>
            {t('user.wizard.addkey.label')}
            <input
              ref={labelField}
              value={label}
              maxLength={200}
              onChange={(event) => setLabel(event.target.value)}
              onBlur={() => touch('label')}
              aria-invalid={labelError}
              aria-describedby={labelError ? 'wizard-addkey-label-err' : undefined}
              autoComplete="off"
              data-testid="wizard-addkey-label"
            />
          </label>
          {labelError ? (
            <small id="wizard-addkey-label-err" className={styles.fieldError}>
              <Icon name="alert" size={14} /> {t('user.wizard.addkey.error.label')}
            </small>
          ) : null}
          <label>
            {t('user.wizard.key.field.key')}
            <input
              ref={keyField}
              className={styles.mono}
              value={key}
              onChange={(event) => {
                setKey(event.target.value);
                setFailure((current) => ({
                  ...current,
                  fields: { ...current.fields, key: undefined },
                }));
              }}
              onBlur={() => touch('key')}
              aria-invalid={keyError !== null || failure.banner === 'invalid'}
              aria-describedby={`wizard-addkey-key-help${keyError ? ' wizard-addkey-key-err' : ''}`}
              autoComplete="off"
              data-testid="wizard-addkey-key"
            />
          </label>
          <small id="wizard-addkey-key-help">{t('user.wizard.key.help.key')}</small>
          {keyError ? (
            <small id="wizard-addkey-key-err" className={styles.fieldError}>
              <Icon name="alert" size={14} />{' '}
              {t(
                keyError === 'duplicate'
                  ? 'user.wizard.key.error.duplicate'
                  : 'user.wizard.key.error.key-format',
              )}
            </small>
          ) : null}
          <label>
            {t('user.wizard.key.field.secret')}
            <span className={styles.secret}>
              <input
                ref={secretField}
                type={showSecret ? 'text' : 'password'}
                className={styles.mono}
                value={secret}
                onChange={(event) => {
                  setSecret(event.target.value);
                  setFailure((current) => ({
                    ...current,
                    fields: { ...current.fields, secret: undefined },
                  }));
                }}
                onBlur={() => touch('secret')}
                aria-invalid={secretError !== null}
                aria-describedby={`wizard-addkey-secret-help${secretError ? ' wizard-addkey-secret-err' : ''}`}
                autoComplete="new-password"
                data-testid="wizard-addkey-secret"
              />
              <button
                type="button"
                aria-label={t(showSecret ? 'user.wizard.key.hide' : 'user.wizard.key.show')}
                aria-pressed={showSecret}
                onClick={() => setShowSecret(!showSecret)}
                data-testid="wizard-addkey-secret-toggle"
              >
                <Icon name="eye" />
              </button>
            </span>
          </label>
          <small id="wizard-addkey-secret-help">{t('user.wizard.key.help.secret')}</small>
          {secretError ? (
            <small id="wizard-addkey-secret-err" className={styles.fieldError}>
              <Icon name="alert" size={14} /> {t('user.wizard.key.error.secret-format')}
            </small>
          ) : null}
          {sending ? (
            <p role="status" className={styles.note}>
              {t('user.wizard.key.checking')}
            </p>
          ) : null}
          <div className={styles.footer}>
            <button
              type="button"
              className={styles.ghost}
              onClick={onClose}
              data-testid="wizard-addkey-cancel"
            >
              {t('common.cancel')}
            </button>
            <button
              type="submit"
              className={styles.primary}
              disabled={sending}
              data-testid="wizard-addkey-submit"
            >
              {t('user.wizard.addkey.submit')}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
