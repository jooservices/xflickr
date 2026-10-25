import { SESSION_EXPIRED_EVENT } from './sessionEvents';
import { ApiShapeError, validateBody } from './validate';

export { ApiShapeError };

const SAFE_METHODS = new Set(['GET', 'HEAD', 'OPTIONS']);

export function readCsrfToken(): string {
  if (typeof document === 'undefined') return '';
  const match = document.cookie.match(/(?:^|;\s*)xf_csrf=([^;]+)/);
  return match ? decodeURIComponent(match[1]) : '';
}

function isCsrfRejection(response: Response): Promise<boolean> {
  if (response.status !== 403) return Promise.resolve(false);
  return response
    .clone()
    .json()
    .then(
      (body: unknown) =>
        typeof body === 'object' &&
        body !== null &&
        'code' in body &&
        body.code === 'csrf_rejected',
    )
    .catch(() => false);
}

function shouldSignalSessionExpired(input: RequestInfo | URL): boolean {
  const path =
    typeof input === 'string' ? input : input instanceof URL ? input.pathname : input.url;
  return path !== '/auth/login' && path !== '/auth/register' && !path.startsWith('/auth/passkey/');
}

export function signalSessionExpired(status: number): void {
  if (status === 401 && typeof window !== 'undefined') {
    window.dispatchEvent(new Event(SESSION_EXPIRED_EVENT));
  }
}

/** Fetches an API response with shared credentials, CSRF header, retry, and session handling. */
export async function csrfFetch(
  input: RequestInfo | URL,
  init: RequestInit = {},
): Promise<Response> {
  const method = (init.method ?? 'GET').toUpperCase();
  const unsafe = !SAFE_METHODS.has(method);
  const headers = new Headers(init.headers);
  headers.set('Accept', 'application/json');

  if (unsafe) {
    const token = readCsrfToken();
    if (token) headers.set('X-CSRF-Token', token);
    else headers.delete('X-CSRF-Token');
  } else {
    headers.delete('X-CSRF-Token');
  }

  const options: RequestInit = { ...init, method, headers, credentials: 'same-origin' };
  let response = await fetch(input, options);

  if (unsafe && (await isCsrfRejection(response))) {
    try {
      await fetch('/me', { method: 'GET', credentials: 'same-origin' });
    } catch {
      // The original request still gets its single retry with the current cookie.
    }
    const freshToken = readCsrfToken();
    if (freshToken) headers.set('X-CSRF-Token', freshToken);
    else headers.delete('X-CSRF-Token');
    response = await fetch(input, options);
  }

  if (response.status === 401 && shouldSignalSessionExpired(input)) {
    signalSessionExpired(response.status);
  }
  return response;
}

/**
 * Parses a response body as JSON. A non-JSON content type (an HTML fallback page served with
 * status 200) or unparsable JSON becomes an `ApiShapeError`. Empty bodies yield `undefined`.
 * Error statuses are parsed leniently because proxies answer them with plain text.
 */
export async function readJsonBody(response: Response): Promise<unknown> {
  const text = [204, 205, 304].includes(response.status) ? '' : await response.text();
  if (text === '') return undefined;
  const contentType = response.headers.get('content-type')?.split(';', 1)[0].trim().toLowerCase();
  const isJson = contentType === 'application/json' || contentType?.endsWith('+json') === true;
  if (!isJson && response.status < 400) {
    throw new ApiShapeError('The server returned a non-JSON response');
  }
  try {
    return JSON.parse(text);
  } catch {
    if (response.status >= 400) return undefined;
    throw new ApiShapeError('The server returned invalid JSON');
  }
}

/** Orval fetch mutator; keeps the generated client's `{ data, status, headers }` result. */
export async function customFetch<T>(input: RequestInfo | URL, init?: RequestInit): Promise<T> {
  const response = await csrfFetch(input, init);
  const path = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
  const method = (init?.method ?? 'GET').toUpperCase();
  const body = validateBody(path, method, response.status, await readJsonBody(response));
  const data = body ?? {};
  return { data, status: response.status, headers: response.headers } as T;
}
