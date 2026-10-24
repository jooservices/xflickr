import { act, renderHook } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { THEME_STORAGE_KEY, resolveTheme, useTheme } from './useTheme';

function stubSystemDark(dark: boolean) {
  vi.stubGlobal('matchMedia', (query: string) => ({
    matches: dark && query.includes('dark'),
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  }));
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('resolveTheme', () => {
  it('defaults ADMIN to dark and USER to the system preference', () => {
    stubSystemDark(false);
    expect(resolveTheme('admin')).toBe('dark');
    expect(resolveTheme('user')).toBe('light');
    stubSystemDark(true);
    expect(resolveTheme('user')).toBe('dark');
  });

  it('prefers the stored choice per role and ignores junk', () => {
    window.localStorage.setItem(`${THEME_STORAGE_KEY}.admin`, 'light');
    window.localStorage.setItem(`${THEME_STORAGE_KEY}.user`, 'sepia');

    expect(resolveTheme('admin')).toBe('light');
    expect(resolveTheme('user')).toBe('light');
  });

  it('works without matchMedia', () => {
    vi.stubGlobal('matchMedia', undefined);

    expect(resolveTheme('user')).toBe('light');
  });
});

describe('useTheme', () => {
  it('toggles and persists the theme', () => {
    const { result } = renderHook(() => useTheme('admin'));

    expect(result.current.theme).toBe('dark');
    act(() => {
      result.current.toggleTheme();
    });
    expect(result.current.theme).toBe('light');
    expect(window.localStorage.getItem(`${THEME_STORAGE_KEY}.admin`)).toBe('light');
  });
});
