import { listFavorites } from '../../shared/api/gen/catalog/catalog';
import { createDownload } from '../../shared/api/gen/default/default';
import type { CatalogFavorite, Download } from '../../shared/api/gen/models';

export const LIBRARY_PAGE_SIZE = 100;
export const LIBRARY_REQUEST_TIMEOUT_MS = 10_000;

export class LibraryApiError extends Error {
  readonly status: number;
  readonly code?: string;

  constructor(status: number, code?: string) {
    super(`Library request failed with ${String(status)}`);
    this.name = 'LibraryApiError';
    this.status = status;
    this.code = code;
  }
}

export type FavoritesPage = {
  items: CatalogFavorite[];
  nextCursor?: string;
};

function requestSignal(signal?: AbortSignal): AbortSignal {
  const timeout = AbortSignal.timeout(LIBRARY_REQUEST_TIMEOUT_MS);
  return signal === undefined ? timeout : AbortSignal.any([signal, timeout]);
}

function failure(status: number, data: unknown): LibraryApiError {
  const code =
    typeof data === 'object' && data !== null && 'code' in data && typeof data.code === 'string'
      ? data.code
      : undefined;
  return new LibraryApiError(status, code);
}

/** Reads one cursor page of the signed-in user's catalog favorites. */
export async function loadFavorites(cursor?: string, signal?: AbortSignal): Promise<FavoritesPage> {
  let response: Awaited<ReturnType<typeof listFavorites>>;
  try {
    response = await listFavorites(
      { ...(cursor === undefined ? {} : { cursor }), limit: LIBRARY_PAGE_SIZE },
      { signal: requestSignal(signal) },
    );
  } catch (error) {
    if (signal?.aborted === true) throw error;
    throw new LibraryApiError(0);
  }
  if (response.status !== 200) throw failure(response.status, response.data);
  return { items: response.data.items, nextCursor: response.data.next_cursor };
}

/** Requests a download for explicit photo ids selected in the local tray. */
export async function createTrayDownload(photoIds: number[]): Promise<Download> {
  let response: Awaited<ReturnType<typeof createDownload>>;
  try {
    response = await createDownload(
      { kind: 'tray', photo_ids: photoIds },
      { signal: AbortSignal.timeout(LIBRARY_REQUEST_TIMEOUT_MS) },
    );
  } catch {
    throw new LibraryApiError(0);
  }
  if (response.status !== 201) throw failure(response.status, response.data);
  return response.data;
}
