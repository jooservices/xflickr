import { getAuthConfig } from '../../shared/api/gen/default/default';
import type { Locale } from '../../shared/i18n/catalog';
import { readCsrfToken, signalSessionExpired } from '../../shared/api/mutator';

export { signalSessionExpired };

export type AuthUser = {
  id: number;
  email: string;
  role: 'user' | 'admin';
  locale: Locale;
  created_at?: string;
};

type ApiResult<T = unknown> = { status: number; data: T; headers: Headers };

export function csrfFetchOptions(method = 'GET'): RequestInit {
  const unsafe = !['GET', 'HEAD', 'OPTIONS'].includes(method);
  const token = readCsrfToken();
  return {
    method,
    credentials: 'same-origin',
    headers: unsafe && token ? { 'X-CSRF-Token': token } : {},
  };
}

/** Narrows a generated-client answer to the body shape the caller reads. */
export function asResult<T>(result: { status: number; data: unknown; headers: Headers }) {
  return result as ApiResult<T>;
}

export function validatedReturnTo(value: string | null): string {
  const path = value?.split(/[?#]/, 1)[0].replace(/\/+$/, '') || '/';
  if (
    value === null ||
    value.length > 2048 ||
    !value.startsWith('/') ||
    value.includes('//') ||
    value.includes('\\') ||
    path === '/login' ||
    path === '/register'
  ) {
    return '/';
  }
  return value;
}

export async function fetchCsrfConfig(): Promise<{
  registration_open: boolean;
  passkey_enabled: boolean;
  terms_url?: string;
  privacy_url?: string;
}> {
  const response = await getAuthConfig();
  if (response.status !== 200) throw new Error('Auth config unavailable');
  const sameOriginPath = (value: string | undefined) =>
    value?.startsWith('/') && !value.startsWith('//') && !value.includes('\\') ? value : undefined;
  return {
    registration_open: response.data.registration_open ?? true,
    passkey_enabled: response.data.passkey_enabled ?? false,
    terms_url: sameOriginPath(response.data.terms_url),
    privacy_url: sameOriginPath(response.data.privacy_url),
  };
}
