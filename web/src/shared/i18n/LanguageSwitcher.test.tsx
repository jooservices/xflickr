import { fireEvent, render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it } from 'vitest';
import { I18nProvider, LOCALE_STORAGE_KEY } from './index';
import { LanguageSwitcher } from './LanguageSwitcher';

describe('LanguageSwitcher', () => {
  beforeEach(() => {
    window.localStorage.clear();
  });

  it('defaults to English and persists Vietnamese after a click', () => {
    render(
      <I18nProvider>
        <LanguageSwitcher testId="lang" />
      </I18nProvider>,
    );

    expect(screen.getByTestId('lang')).toHaveTextContent('EN');
    expect(document.documentElement.lang).toBe('en');

    fireEvent.click(screen.getByTestId('lang'));

    expect(screen.getByTestId('lang')).toHaveTextContent('VI');
    expect(document.documentElement.lang).toBe('vi');
    expect(window.localStorage.getItem(LOCALE_STORAGE_KEY)).toBe('vi');
  });

  it('honours initialLocale for tests and screenshots', () => {
    render(
      <I18nProvider initialLocale="vi">
        <LanguageSwitcher testId="lang" />
      </I18nProvider>,
    );
    expect(screen.getByTestId('lang')).toHaveTextContent('VI');
  });
});
