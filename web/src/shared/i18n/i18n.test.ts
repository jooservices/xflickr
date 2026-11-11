import { beforeEach, describe, expect, it, vi } from 'vitest';
import { catalogs, i18nInstance, translate, translateErrorCode } from './init';

describe('i18n catalogs', () => {
  it('keeps identical key sets for English and Vietnamese', () => {
    expect(Object.keys(catalogs.vi).sort()).toEqual(Object.keys(catalogs.en).sort());
  });

  it('defaults to English and falls back when Vietnamese is missing at runtime', async () => {
    await i18nInstance.changeLanguage('en');
    expect(translate('shell.nav.library')).toBe('Library');
    expect(translate('en', 'shell.nav.library')).toBe('Library');
    expect(translate('vi', 'shell.nav.library')).toBe('Thư viện');

    await i18nInstance.changeLanguage('vi');
    expect(translate('shell.nav.library')).toBe('Thư viện');
  });

  it('interpolates values with {{name}} placeholders', async () => {
    await i18nInstance.changeLanguage('en');
    expect(translate('shell.live.reason', { reason: 'offline' })).toBe('Paused: offline');
  });

  it('selects plural forms through count', async () => {
    await i18nInstance.changeLanguage('en');
    expect(translate('shell.notifications.button', { count: 0 })).toBe('Notifications');
    expect(translate('shell.notifications.button', { count: 1 })).toBe('Notifications, 1 unread');
    expect(translate('shell.notifications.button', { count: 3 })).toBe('Notifications, 3 unread');
  });
});

describe('translateErrorCode', () => {
  beforeEach(async () => {
    await i18nInstance.changeLanguage('en');
  });

  it('maps known codes and falls back to a generic message', () => {
    expect(translateErrorCode('key_invalid')).toMatch(/Flickr/i);
    expect(translateErrorCode('/problems/forbidden')).toMatch(/permission/i);
    expect(translateErrorCode('totally_unknown_code')).toBe(catalogs.en['errors.generic']);
    expect(translateErrorCode(undefined)).toBe(catalogs.en['errors.generic']);
  });
});

describe('locale storage helpers', () => {
  it('recognizes stored locale values and switches in both directions', async () => {
    const { isLocale, otherLocale, readStoredLocale, LOCALE_STORAGE_KEY } = await import('./init');
    window.localStorage.setItem(LOCALE_STORAGE_KEY, 'vi');

    expect(readStoredLocale()).toBe('vi');
    expect(isLocale('en')).toBe(true);
    expect(isLocale('vi')).toBe(true);
    expect(isLocale('fr')).toBe(false);
    expect(isLocale(42)).toBe(false);
    expect(otherLocale('en')).toBe('vi');
    expect(otherLocale('vi')).toBe('en');
  });

  it('reads and ignores invalid stored locales', async () => {
    const { readStoredLocale, persistLocale, LOCALE_STORAGE_KEY, splitMessageKey } =
      await import('./init');
    window.localStorage.setItem(LOCALE_STORAGE_KEY, 'fr');
    expect(readStoredLocale()).toBe('en');
    persistLocale('vi');
    expect(window.localStorage.getItem(LOCALE_STORAGE_KEY)).toBe('vi');
    expect(splitMessageKey('shell.nav.library')).toEqual({ ns: 'shell', key: 'nav.library' });
    expect(splitMessageKey('orphan')).toEqual({ ns: 'common', key: 'orphan' });
  });

  it('tolerates blocked localStorage', async () => {
    const { readStoredLocale, persistLocale } = await import('./init');
    const getItem = vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('blocked');
    });
    const setItem = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('blocked');
    });
    expect(readStoredLocale()).toBe('en');
    expect(() => persistLocale('vi')).not.toThrow();
    getItem.mockRestore();
    setItem.mockRestore();
  });
});
