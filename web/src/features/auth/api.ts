import type { Locale } from '../../shared/i18n/catalog';
import { csrfFetch, readCsrfToken, signalSessionExpired } from '../../shared/api/mutator';

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

export async function request<T>(
  path: string,
  method = 'GET',
  body?: unknown,
  signal?: AbortSignal,
): Promise<ApiResult<T>> {
  const response = await csrfFetch(path, {
    method,
    signal,
    headers: {
      ...(body === undefined ? {} : { 'Content-Type': 'application/json' }),
    },
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
  });
  const text = response.status === 204 ? '' : await response.text();
  let data: T = {} as T;
  if (text) {
    try {
      data = JSON.parse(text) as T;
    } catch {
      data = {} as T;
    }
  }
  return { status: response.status, data, headers: response.headers };
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
  const response = await request<{
    registration_open?: boolean;
    passkey_enabled?: boolean;
    terms_url?: string;
    privacy_url?: string;
  }>('/auth/config');
  if (response.status < 200 || response.status >= 300) throw new Error('Auth config unavailable');
  const sameOriginPath = (value: string | undefined) =>
    value?.startsWith('/') && !value.startsWith('//') && !value.includes('\\') ? value : undefined;
  return {
    registration_open: response.data.registration_open ?? true,
    passkey_enabled: response.data.passkey_enabled ?? false,
    terms_url: sameOriginPath(response.data.terms_url),
    privacy_url: sameOriginPath(response.data.privacy_url),
  };
}
