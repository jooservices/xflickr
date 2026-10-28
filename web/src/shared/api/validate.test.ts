import { describe, expect, it } from 'vitest';
import * as contract from './zod/schemas';
import { ApiShapeError, ROUTES, validateBody } from './validate';

const key = {
  id: 1,
  label: 'Main',
  key_public: 'abc123************',
  per_hour_limit: 3600,
  per_second_limit: 1,
  burst: 10,
  status: 'valid',
  created_at: '2026-01-01T00:00:00Z',
};

describe('validateBody', () => {
  it('returns bodies that match the schema of their operation', () => {
    expect(validateBody('/keys', 'GET', 200, [key])).toEqual([key]);
    expect(validateBody('/keys/7?x=1', 'GET', 200, key)).toEqual(key);
    expect(validateBody('/flickr-accounts', 'GET', 200, [])).toEqual([]);
    expect(
      validateBody('/readyz', 'GET', 200, {
        status: 'ok',
        dependencies: [{ name: 'api.postgres', status: 'ok' }],
      }),
    ).toEqual({ status: 'ok', dependencies: [{ name: 'api.postgres', status: 'ok' }] });
    expect(validateBody('/keys/9', 'DELETE', 204, undefined)).toBeUndefined();
    expect(validateBody('/auth/logout', 'POST', 200, undefined)).toBeUndefined();
  });

  it.each([
    ['/keys', 'GET', {}],
    ['/keys', 'GET', null],
    ['/keys', 'GET', [{ id: 'x' }]],
    ['/keys/7', 'GET', [key]],
    ['/flickr-accounts', 'GET', null],
    ['/flickr-accounts', 'GET', { items: [] }],
    ['/me/crawl/settings', 'GET', { depth_max: 'x' }],
    ['/me', 'GET', {}],
    ['/unknown/path', 'GET', []],
    ['/unknown/path', 'GET', 'html'],
  ])('rejects %s %s answering %j', (path, method, body) => {
    expect(() => validateBody(path, method, 200, body)).toThrow(ApiShapeError);
  });

  it('rejects an empty or unexpected body on success statuses', () => {
    expect(() => validateBody('/keys', 'GET', 200, undefined)).toThrow(ApiShapeError);
    expect(() => validateBody('/keys/1', 'DELETE', 204, {})).toThrow(ApiShapeError);
  });

  it('keeps HTTP error handling for error statuses', () => {
    expect(validateBody('/keys', 'GET', 500, undefined)).toBeUndefined();
    expect(validateBody('/keys', 'GET', 403, { code: 'forbidden' })).toEqual({ code: 'forbidden' });
    expect(() => validateBody('/keys', 'GET', 502, [])).toThrow(ApiShapeError);
  });

  it('accepts an object from a path without a schema', () => {
    expect(validateBody('/auth/passkey/options', 'POST', 200, { challenge: 'x' })).toEqual({
      challenge: 'x',
    });
  });

  it('has a route for every generated response schema', () => {
    const generated = Object.entries(contract)
      .filter(([name]) => name.endsWith('Response'))
      .map(([, schema]) => schema);
    const registered = new Set<unknown>(ROUTES.map((route) => route.schema));
    const missing = generated.filter((schema) => !registered.has(schema));
    expect(missing).toEqual([]);
  });
});
