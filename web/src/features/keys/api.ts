import { createKey, listKeys } from '../../shared/api/gen/keys/keys';
import type { CreateKeyRequest, FlickrAccount } from '../../shared/api/gen/models';

export async function loadKeys(signal?: AbortSignal) {
  const response = await listKeys({ signal });
  if (response.status !== 200)
    throw Object.assign(new Error('Could not load keys'), { status: response.status });
  return response.data;
}

export async function checkAndSaveKey(request: CreateKeyRequest) {
  return createKey(request);
}

/**
 * Lists the current user's Flickr accounts. `GET /flickr-accounts` is a contract
 * request that the build tickets add to OpenAPI, so it is read by hand here.
 * Returns `null` while the server does not offer the list yet (404/405/501).
 */
export async function loadAccounts(signal?: AbortSignal): Promise<FlickrAccount[] | null> {
  const response = await fetch('/flickr-accounts', { signal });
  if ([404, 405, 501].includes(response.status)) return null;
  if (!response.ok)
    throw Object.assign(new Error('Could not load accounts'), { status: response.status });
  return (await response.json()) as FlickrAccount[];
}
