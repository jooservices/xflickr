import { useCallback, useState } from 'react';

export type ThemeRole = 'user' | 'admin';
export type Theme = 'light' | 'dark';

export const THEME_STORAGE_KEY = 'xf.theme';

function storageKey(role: ThemeRole): string {
  return `${THEME_STORAGE_KEY}.${role}`;
}

function systemTheme(): Theme {
  return typeof window.matchMedia === 'function' &&
    window.matchMedia('(prefers-color-scheme: dark)').matches
    ? 'dark'
    : 'light';
}

/** Stored choice first; otherwise USER follows the system and ADMIN defaults to dark. */
export function resolveTheme(role: ThemeRole): Theme {
  try {
    const stored = window.localStorage.getItem(storageKey(role));
    if (stored === 'light' || stored === 'dark') return stored;
  } catch {
    // Storage can be blocked; fall through to the role default.
  }
  return role === 'admin' ? 'dark' : systemTheme();
}

/** Theme of one role, persisted per role in localStorage until the profile stores it. */
export function useTheme(role: ThemeRole): { theme: Theme; toggleTheme: () => void } {
  const [theme, setTheme] = useState<Theme>(() => resolveTheme(role));

  const toggleTheme = useCallback(() => {
    const next: Theme = theme === 'dark' ? 'light' : 'dark';
    setTheme(next);
    try {
      window.localStorage.setItem(storageKey(role), next);
    } catch {
      // The choice then lasts for this page only.
    }
  }, [role, theme]);

  return { theme, toggleTheme };
}
