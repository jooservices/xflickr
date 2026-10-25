import { http, HttpResponse } from 'msw/http';
import { describe, expect, it } from 'vitest';
import { apiKeyBody } from '../../test/fixtures';
import { server } from '../../test/server';
import {
  checkAndSaveKey,
  loadAccounts,
  loadCrawlSettings,
  loadKeys,
  saveKey,
  startCrawl,
} from './api';

const body = { label: 'My key', key: 'public', secret: 'secret' };
const post = (status: number, payload: Record<string, unknown> = {}) =>
  server.use(http.post('*/keys', () => HttpResponse.json(payload, { status })));

describe('key API', () => {
  it('loads the current user keys', async () => {
    server.use(http.get('*/keys', () => HttpResponse.json([])));
    await expect(loadKeys()).resolves.toEqual([]);
  });

  it('reports non-success key list responses with their status', async () => {
    server.use(http.get('*/keys', () => HttpResponse.json({ code: 'forbidden' }, { status: 403 })));
    await expect(loadKeys()).rejects.toMatchObject({ status: 403 });
  });

  it('posts a key for the server to validate and store', async () => {
    server.use(
      http.post('*/keys', async ({ request }) => {
        expect(await request.json()).toMatchObject(body);
        return HttpResponse.json(apiKeyBody(), { status: 201 });
      }),
    );
    await expect(checkAndSaveKey(body)).resolves.toMatchObject({ status: 201 });
  });
});

describe('contract requests', () => {
  it('loads the account list and reports every failure', async () => {
    server.use(http.get('*/flickr-accounts', () => HttpResponse.json([{ id: 1 }])));
    await expect(loadAccounts()).resolves.toEqual([{ id: 1 }]);

    for (const status of [403, 404, 405, 501, 500]) {
      server.use(http.get('*/flickr-accounts', () => new HttpResponse(null, { status })));
      await expect(loadAccounts()).rejects.toMatchObject({ status });
    }
  });

  it('loads the crawl settings and reports failures', async () => {
    const settings = { depth: 2, depth_default: 2, depth_max: 4 };
    server.use(http.get('*/me/crawl/settings', () => HttpResponse.json(settings)));
    await expect(loadCrawlSettings()).resolves.toEqual(settings);

    server.use(http.get('*/me/crawl/settings', () => new HttpResponse(null, { status: 404 })));
    await expect(loadCrawlSettings()).rejects.toMatchObject({ status: 404 });
  });

  it('stores the depth with the CSRF header', async () => {
    document.cookie = 'xf_csrf=tok';
    server.use(
      http.put('*/me/crawl/settings', async ({ request }) => {
        expect(request.headers.get('X-CSRF-Token')).toBe('tok');
        expect(await request.json()).toEqual({ depth: 3 });
        return HttpResponse.json({ depth: 3, depth_default: 2, depth_max: 4 });
      }),
    );
    await expect(startCrawl(3)).resolves.toEqual({ status: 200, code: undefined });

    server.use(
      http.put('*/me/crawl/settings', () =>
        HttpResponse.json({ code: 'depth_above_cap' }, { status: 422 }),
      ),
    );
    await expect(startCrawl(9)).resolves.toEqual({ status: 422, code: 'depth_above_cap' });
  });
});

describe('saveKey', () => {
  it('returns the stored key on 201', async () => {
    post(201, apiKeyBody({ id: 7 }));
    await expect(saveKey(body)).resolves.toEqual({ key: apiKeyBody({ id: 7 }) });
  });

  it('maps field errors, duplicate, invalid, no node and other failures', async () => {
    post(400, { code: 'validation_failed', details: { fields: { key: 'x', secret: 'y' } } });
    await expect(saveKey(body)).resolves.toEqual({
      failure: { fields: { key: 'format', secret: 'format' } },
    });
    post(400, { code: 'validation_failed' });
    await expect(saveKey(body)).resolves.toEqual({ failure: { banner: 'network' } });
    post(409, { code: 'key_duplicate' });
    await expect(saveKey(body)).resolves.toEqual({ failure: { fields: { key: 'duplicate' } } });
    post(422, { code: 'key_invalid' });
    await expect(saveKey(body)).resolves.toEqual({ failure: { banner: 'invalid' } });
    post(422, { code: 'other' });
    await expect(saveKey(body)).resolves.toEqual({ failure: { banner: 'network' } });
    post(503, { code: 'no_node_available' });
    await expect(saveKey(body)).resolves.toEqual({ failure: { banner: 'node' } });
    post(429, { code: 'rate_limited' });
    await expect(saveKey(body)).resolves.toEqual({ failure: { banner: 'network' } });
    server.use(http.post('*/keys', () => HttpResponse.error()));
    await expect(saveKey(body)).resolves.toEqual({ failure: { banner: 'network' } });
  });
});
