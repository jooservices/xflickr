import { act, renderHook } from '@testing-library/react';
import { createElement } from 'react';
import { useState, type ReactNode } from 'react';
import { I18nextProvider } from 'react-i18next';
import { http, HttpResponse } from 'msw';
import { expect, it, vi } from 'vitest';
import { I18nBridge, i18nInstance } from '../../shared';
import { server } from '../../test/server';
import { useKeyForm } from './useKeyForm';
import type { KeyDraft } from './useSetupData';

const KEY = '0123456789abcdef0123456789abcdef';
const SECRET = '0123456789abcdef';

const acceptedKey = {
  id: 5,
  label: 'My Flickr key',
  key_public: 'abcdef••••',
  per_hour_limit: 3600,
  per_second_limit: 4,
  burst: 8,
  status: 'valid',
  created_at: '2026-10-25T00:00:00Z',
};

function wrapper({ children }: { children: ReactNode }) {
  return createElement(
    I18nextProvider,
    { i18n: i18nInstance },
    createElement(I18nBridge, { initialLocale: 'en', children }),
  );
}

function renderKeyForm() {
  const onSaved = vi.fn();
  const hook = renderHook(
    () => {
      const [draft, setDraft] = useState<KeyDraft>({ key: '', secret: '' });
      return useKeyForm({ onSaved, draft, setDraft });
    },
    { wrapper },
  );
  return { ...hook, onSaved };
}

it('useKeyForm shows no format error until the key field is left', () => {
  const { result } = renderKeyForm();

  act(() => result.current.setField('key', 'not-a-key'));
  expect(result.current.keyError).toBeNull();

  act(() => result.current.blurField('key'));
  expect(result.current.keyError).toBe('format');

  act(() => result.current.setField('key', KEY));
  expect(result.current.keyError).toBeNull();
});

it('useKeyForm trims spaces and line breaks from both fields before it sends them', async () => {
  const sent: unknown[] = [];
  server.use(
    http.post('*/keys', async ({ request }) => {
      sent.push(await request.json());
      return HttpResponse.json(acceptedKey, { status: 201 });
    }),
  );
  const { result, onSaved } = renderKeyForm();

  act(() => {
    result.current.setField('key', ` ${KEY.slice(0, 16)}\n${KEY.slice(16)} `);
    result.current.setField('secret', `\t${SECRET.slice(0, 8)} ${SECRET.slice(8)}\n`);
  });
  await act(() => result.current.submit());

  expect(sent).toEqual([expect.objectContaining({ key: KEY, secret: SECRET })]);
  expect(onSaved).toHaveBeenCalledWith(acceptedKey);
  expect(result.current.key).toBe('');
  expect(result.current.secret).toBe('');
});

it('useKeyForm maps the duplicate-key answer to a key field error that clears on edit', async () => {
  server.use(
    http.post('*/keys', () => HttpResponse.json({ code: 'key_duplicate' }, { status: 409 })),
  );
  const { result, onSaved } = renderKeyForm();

  act(() => {
    result.current.setField('key', KEY);
    result.current.setField('secret', SECRET);
  });
  await act(() => result.current.submit());

  expect(result.current.keyError).toBe('duplicate');
  expect(result.current.secretError).toBeNull();
  expect(result.current.banner).toBeNull();
  expect(onSaved).not.toHaveBeenCalled();

  act(() => result.current.setField('key', 'fedcba9876543210fedcba9876543210'));
  expect(result.current.keyError).toBeNull();
});
