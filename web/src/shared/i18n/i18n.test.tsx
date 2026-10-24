import { act, fireEvent, render, renderHook, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { catalogs } from './catalog';
import { formatMessage } from './format';
import { I18nProvider, LOCALE_STORAGE_KEY, translate, useI18n } from './I18nProvider';
import type { ReactNode } from 'react';

describe('formatMessage', () => {
  it('replaces named placeholders and leaves unknown ones as written', () => {
    expect(formatMessage('Hi {name} {missing}', { name: 'Ann' })).toBe('Hi Ann {missing}');
  });

  it('selects ICU plural cases, exact zero first, and formats the count', () => {
    const message = '{count, plural, =0 {None} one {# photo} other {# photos}}';

    expect(formatMessage(message, { count: 0 }, 'en')).toBe('None');
    expect(formatMessage(message, { count: 1 }, 'en')).toBe('1 photo');
    expect(formatMessage(message, { count: 1284 }, 'en')).toBe('1,284 photos');
    expect(formatMessage(message, { count: 1284 }, 'vi')).toBe('1.284 photos');
  });

  it('falls back to other and keeps surrounding text and nested placeholders', () => {
    const message = 'Sync: {count, plural, other {# of {total}}} done';

    expect(formatMessage(message, { count: 3, total: 9 }, 'vi')).toBe('Sync: 3 of 9 done');
  });

  it('tolerates an unbalanced brace', () => {
    expect(formatMessage('Broken {name', { name: 'x' })).toBe('Broken {name');
  });
});

describe('catalogs', () => {
  it('has a Vietnamese string for every English key and no extras', () => {
    expect(Object.keys(catalogs.vi).sort()).toEqual(Object.keys(catalogs.en).sort());
  });

  it('defines the other case in every plural message', () => {
    for (const locale of ['en', 'vi'] as const) {
      for (const [key, message] of Object.entries(catalogs[locale])) {
        if (message.includes(', plural,')) {
          expect(message, `${locale}:${key}`).toContain('other {');
        }
      }
    }
  });

  it('translates and falls back to the key when it is unknown', () => {
    expect(translate('vi', 'shell.nav.library')).toBe('Thư viện');
    expect(translate('en', 'no.such.key' as never)).toBe('no.such.key');
  });
});

function Probe() {
  const { t, locale, toggleLocale } = useI18n();
  return (
    <button type="button" onClick={toggleLocale}>
      {locale}:{t('shell.nav.library')}
    </button>
  );
}

describe('I18nProvider', () => {
  it('switches language, persists it and sets <html lang>', () => {
    render(
      <I18nProvider>
        <Probe />
      </I18nProvider>,
    );

    expect(screen.getByRole('button')).toHaveTextContent('en:Library');
    expect(document.documentElement.lang).toBe('en');

    fireEvent.click(screen.getByRole('button'));

    expect(screen.getByRole('button')).toHaveTextContent('vi:Thư viện');
    expect(document.documentElement.lang).toBe('vi');
    expect(window.localStorage.getItem(LOCALE_STORAGE_KEY)).toBe('vi');
  });

  it('starts from the stored language and ignores an invalid value', () => {
    window.localStorage.setItem(LOCALE_STORAGE_KEY, 'vi');
    const stored = render(
      <I18nProvider>
        <Probe />
      </I18nProvider>,
    );
    expect(screen.getByRole('button')).toHaveTextContent('vi:');
    stored.unmount();

    window.localStorage.setItem(LOCALE_STORAGE_KEY, 'fr');
    render(
      <I18nProvider>
        <Probe />
      </I18nProvider>,
    );
    expect(screen.getByRole('button')).toHaveTextContent('en:');
  });

  it('exposes setLocale and throws outside the provider', () => {
    const wrapper = ({ children }: { children: ReactNode }) => (
      <I18nProvider>{children}</I18nProvider>
    );
    const { result } = renderHook(() => useI18n(), { wrapper });

    act(() => {
      result.current.setLocale('vi');
    });
    expect(result.current.locale).toBe('vi');
    expect(() => renderHook(() => useI18n())).toThrow(/inside I18nProvider/);
  });
});
