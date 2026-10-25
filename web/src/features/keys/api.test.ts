import { http, HttpResponse } from 'msw/http';
import { describe, expect, it } from 'vitest';
import { server } from '../../test/server';
import { checkAndSaveKey, loadAccounts, loadKeys } from './api';

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
        expect(await request.json()).toMatchObject({
          label: 'My key',
          key: 'public',
          secret: 'secret',
        });
        return HttpResponse.json({ id: 1 }, { status: 201 });
      }),
    );
    await expect(
      checkAndSaveKey({ label: 'My key', key: 'public', secret: 'secret' }),
    ).resolves.toMatchObject({ status: 201 });
  });

  it('loads the account list, treats a missing endpoint as unknown, and reports failures', async () => {
    server.use(http.get('*/flickr-accounts', () => HttpResponse.json([{ id: 1 }])));
    await expect(loadAccounts()).resolves.toEqual([{ id: 1 }]);

    server.use(http.get('*/flickr-accounts', () => new HttpResponse(null, { status: 404 })));
    await expect(loadAccounts()).resolves.toBeNull();

    server.use(http.get('*/flickr-accounts', () => new HttpResponse(null, { status: 403 })));
    await expect(loadAccounts()).rejects.toMatchObject({ status: 403 });
  });
});
