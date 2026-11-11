import { http, HttpResponse } from 'msw/http';
import { describe, expect, it } from 'vitest';
import { server } from '../../test/server';
import { createTrayDownload, LibraryApiError, loadFavorites } from './api';

describe('library API', () => {
  it('returns the cursor page from the catalog endpoint', async () => {
    server.use(
      http.get('*/favorites', ({ request }) => {
        expect(new URL(request.url).searchParams.get('limit')).toBe('100');
        return HttpResponse.json({ items: [], next_cursor: 'next-page' });
      }),
    );

    await expect(loadFavorites()).resolves.toEqual({ items: [], nextCursor: 'next-page' });
  });

  it('maps API failures without exposing a response body', async () => {
    server.use(
      http.get('*/favorites', () =>
        HttpResponse.json({ title: 'private detail' }, { status: 503 }),
      ),
    );

    await expect(loadFavorites()).rejects.toMatchObject({
      name: 'LibraryApiError',
      status: 503,
      code: undefined,
      message: 'Library request failed with 503',
    });
  });

  it('converts a catalog network failure to a safe API error', async () => {
    server.use(http.get('*/favorites', () => HttpResponse.error()));

    await expect(loadFavorites()).rejects.toMatchObject({ status: 0 });
  });

  it('preserves caller cancellation for catalog requests', async () => {
    const controller = new AbortController();
    const reason = new Error('request cancelled');
    controller.abort(reason);

    await expect(loadFavorites(undefined, controller.signal)).rejects.toBe(reason);
  });

  it('maps download transport failures to a safe API error', async () => {
    server.use(http.post('*/downloads', () => HttpResponse.error()));

    await expect(createTrayDownload([1])).rejects.toBeInstanceOf(LibraryApiError);
    await expect(createTrayDownload([1])).rejects.toMatchObject({ status: 0 });
  });
});
