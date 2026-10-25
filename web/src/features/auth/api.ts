import type { Locale } from '../../shared/i18n/catalog';
import { SESSION_EXPIRED_EVENT } from '../../shared/api/sessionEvents';

export type AuthUser = {
  id: number;
  email: string;
  role: 'user' | 'admin';
  locale: Locale;
  created_at?: string;
};

type ApiResult<T = unknown> = { status: number; data: T; headers: Headers };

function csrfToken(): string {
  const match = document.cookie.match(/(?:^|;\s*)xf_csrf=([^;]+)/);
  return match ? decodeURIComponent(match[1]) : '';
}

export async function request<T>(
  path: string,
  method = 'GET',
  body?: unknown,
): Promise<ApiResult<T>> {
  const unsafe = !['GET', 'HEAD', 'OPTIONS'].includes(method);
  const response = await fetch(path, {
    method,
    credentials: 'same-origin',
    headers: {
      ...(body === undefined ? {} : { 'Content-Type': 'application/json' }),
      ...(unsafe && csrfToken() ? { 'X-CSRF-Token': csrfToken() } : {}),
    },
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
  });
  const text = response.status === 204 ? '' : await response.text();
  if (
    response.status === 401 &&
    path !== '/auth/login' &&
    path !== '/auth/register' &&
    typeof window !== 'undefined'
  ) {
    window.dispatchEvent(new Event(SESSION_EXPIRED_EVENT));
  }
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
  if (
    value === null ||
    value.length > 2048 ||
    !value.startsWith('/') ||
    value.startsWith('//') ||
    value.includes('\\') ||
    value === '/login' ||
    value === '/register'
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
