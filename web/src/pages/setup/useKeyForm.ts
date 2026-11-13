import { useRef, useState } from 'react';
import type { Dispatch, FormEvent, RefObject, SetStateAction } from 'react';
import type { ApiKey } from '../../shared/api/gen/models';
import { useI18n } from '../../shared';
import { clean, keyPattern, saveKey, secretPattern } from '../../features/keys/api';
import type { KeyFailure } from '../../features/keys/api';
import { randomHex } from './setupModel';
import type { FieldError, KeyBanner } from './setupModel';
import type { KeyDraft } from './useSetupData';

type Field = 'key' | 'secret';
type ServerErrors = { key?: FieldError; secret?: FieldError };
type Inputs = Record<Field, RefObject<HTMLInputElement | null>>;

export type KeyForm = {
  key: string;
  secret: string;
  showSecret: boolean;
  setShowSecret: Dispatch<SetStateAction<boolean>>;
  keyError: FieldError | null;
  secretError: FieldError | null;
  banner: KeyBanner | null;
  sending: boolean;
  keyField: RefObject<HTMLInputElement | null>;
  secretField: RefObject<HTMLInputElement | null>;
  bannerRef: RefObject<HTMLDivElement | null>;
  setField: (field: Field, value: string) => void;
  blurField: (field: Field) => void;
  submit: (event?: FormEvent) => Promise<void>;
  fillExample: () => void;
  clearBanner: () => void;
};

/** The values and feedback of the add-key form; only the handlers below write them. */
function useKeyState(draft: KeyDraft, setDraft: Dispatch<SetStateAction<KeyDraft>>) {
  const [showSecret, setShowSecret] = useState(false);
  const [touched, setTouched] = useState({ key: false, secret: false });
  const [submitted, setSubmitted] = useState(false);
  const [serverErrors, setServerErrors] = useState<ServerErrors>({});
  const [banner, setBanner] = useState<KeyBanner | null>(null);
  const [sending, setSending] = useState(false);
  return {
    key: draft.key,
    setKey: (key: string) => setDraft((current) => ({ ...current, key })),
    secret: draft.secret,
    setSecret: (secret: string) => setDraft((current) => ({ ...current, secret })),
    showSecret,
    setShowSecret,
    touched,
    setTouched,
    submitted,
    setSubmitted,
    serverErrors,
    setServerErrors,
    banner,
    setBanner,
    sending,
    setSending,
  };
}

type KeyState = ReturnType<typeof useKeyState>;
type KeyDeps = { state: KeyState; label: string; inputs: Inputs; onSaved: (key: ApiKey) => void };

/** Format check of both fields; spaces and line breaks are not part of the value. */
function formatInvalid(key: string, secret: string) {
  return { key: !keyPattern.test(clean(key)), secret: !secretPattern.test(clean(secret)) };
}

/** A field shows its server answer, else a format error once it was left or submitted. */
function fieldErrors({ key, secret, touched, submitted, serverErrors }: KeyState): {
  keyError: FieldError | null;
  secretError: FieldError | null;
} {
  const invalid = formatInvalid(key, secret);
  return {
    keyError: serverErrors.key ?? ((touched.key || submitted) && invalid.key ? 'format' : null),
    secretError:
      serverErrors.secret ?? ((touched.secret || submitted) && invalid.secret ? 'format' : null),
  };
}

const focusLater = (field: RefObject<HTMLInputElement | null>) => {
  window.setTimeout(() => field.current?.focus(), 0);
};

/** A typed value clears the server answer for its own field. */
function setFieldValue(state: KeyState, field: Field, value: string) {
  if (field === 'key') state.setKey(value);
  else state.setSecret(value);
  state.setServerErrors((current) => ({ ...current, [field]: undefined }));
}

/** After a save the form is empty again, with no feedback left from the last try. */
function clearAfterSave(state: KeyState) {
  state.setKey('');
  state.setSecret('');
  state.setShowSecret(false);
  state.setTouched({ key: false, secret: false });
  state.setSubmitted(false);
}

/** Maps a failed save to the banner and to the field errors, focusing the field it names. */
function showFailure({ state, inputs }: KeyDeps, { banner, fields }: KeyFailure) {
  if (banner) state.setBanner(banner);
  if (fields) {
    state.setServerErrors(fields);
    focusLater(inputs[fields.key ? 'key' : 'secret']);
  }
}

/** Sends the trimmed values; a client format error focuses its field and sends nothing. */
async function submitKey(deps: KeyDeps, event?: FormEvent) {
  event?.preventDefault();
  const { state, inputs, label, onSaved } = deps;
  if (state.sending) return;
  state.setSubmitted(true);
  state.setBanner(null);
  state.setServerErrors({});
  const invalid = formatInvalid(state.key, state.secret);
  if (invalid.key || invalid.secret) {
    focusLater(inputs[invalid.key ? 'key' : 'secret']);
    return;
  }
  state.setSending(true);
  const result = await saveKey({ label, key: clean(state.key), secret: clean(state.secret) });
  state.setSending(false);
  if ('key' in result) {
    clearAfterSave(state);
    onSaved(result.key);
  } else showFailure(deps, result.failure);
}

/** The demo build fills both fields with random values of the right length. */
function fillSample(state: KeyState) {
  state.setKey(randomHex(32));
  state.setSecret(randomHex(16));
  state.setServerErrors({});
  state.setBanner(null);
}

/** State, validation and submit of the add-key form on step 1 of the setup wizard. */
export function useKeyForm({
  onSaved,
  draft,
  setDraft,
}: {
  onSaved: (key: ApiKey) => void;
  draft: KeyDraft;
  setDraft: Dispatch<SetStateAction<KeyDraft>>;
}): KeyForm {
  const { t } = useI18n();
  const state = useKeyState(draft, setDraft);
  const keyField = useRef<HTMLInputElement>(null);
  const secretField = useRef<HTMLInputElement>(null);
  const bannerRef = useRef<HTMLDivElement>(null);
  const deps: KeyDeps = {
    state,
    label: t('setup.key.default-label'),
    inputs: { key: keyField, secret: secretField },
    onSaved,
  };
  const errors = fieldErrors(state);
  return {
    key: state.key,
    secret: state.secret,
    showSecret: state.showSecret,
    setShowSecret: state.setShowSecret,
    keyError: errors.keyError,
    secretError: errors.secretError,
    banner: state.banner,
    sending: state.sending,
    keyField,
    secretField,
    bannerRef,
    setField: (field, value) => setFieldValue(state, field, value),
    blurField: (field) => state.setTouched((current) => ({ ...current, [field]: true })),
    submit: (event) => submitKey(deps, event),
    fillExample: () => fillSample(state),
    clearBanner: () => state.setBanner(null),
  };
}
