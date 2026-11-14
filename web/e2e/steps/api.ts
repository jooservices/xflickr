import { expect } from '@playwright/test';
import type { APIRequestContext, Page } from '@playwright/test';
import { fakeFlickrUrl } from '../support/env';

type Method = 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE';
export type ApiResult = { status: number; body: unknown };

/** Reads the API from Chromium so Secure same-origin cookies behave as they do in the app. */
export async function apiRead(page: Page, path: string): Promise<ApiResult> {
  return browserRequest(page, 'GET', path);
}

/** A write call with the CSRF header the app sends (the token is the readable cookie set at sign-in). */
export async function apiWrite(
  page: Page,
  method: Exclude<Method, 'GET'>,
  path: string,
  data?: unknown,
): Promise<ApiResult> {
  return browserRequest(page, method, path, data);
}

async function browserRequest(
  page: Page,
  method: Method,
  path: string,
  data?: unknown,
): Promise<ApiResult> {
  return page.evaluate(
    async ({ requestMethod, requestPath, body }) => {
      const csrfCookie = document.cookie
        .split(';')
        .map((cookie) => cookie.trim())
        .find((cookie) => cookie.startsWith('xf_csrf='));
      const csrf = csrfCookie ? decodeURIComponent(csrfCookie.slice('xf_csrf='.length)) : '';
      const headers = new Headers({ Accept: 'application/json' });
      if (requestMethod !== 'GET' && csrf) headers.set('X-CSRF-Token', csrf);
      if (body !== undefined) headers.set('Content-Type', 'application/json');
      const response = await fetch(requestPath, {
        method: requestMethod,
        headers,
        credentials: 'same-origin',
        ...(body === undefined ? {} : { body: JSON.stringify(body) }),
      });
      const text = await response.text();
      return {
        status: response.status,
        body: text === '' ? undefined : (JSON.parse(text) as unknown),
      };
    },
    { requestMethod: method, requestPath: path, body: data },
  );
}

/** Reads a JSON resource with the page's own authenticated request context; polling callers tolerate a non-200. */
export async function getJson(
  page: Page,
  path: string,
): Promise<{ status: number; body: unknown }> {
  const response = await page.request.get(path);
  return {
    status: response.status(),
    body: response.status() === 200 ? await response.json() : undefined,
  };
}

/** Points fakeflickr at a scenario (it replaces the previous one). */
export async function setFlickrScenario(
  request: APIRequestContext,
  scenario: Record<string, unknown>,
): Promise<void> {
  const response = await request.post(`${fakeFlickrUrl()}/__admin/scenarios`, { data: scenario });
  expect(response.status(), 'fakeflickr scenario').toBe(204);
}
