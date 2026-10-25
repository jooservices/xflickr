import { connectFlickrAccount } from '../../shared/api/gen/flickr-accounts/flickr-accounts';
import { deleteKey, getKeyQuota } from '../../shared/api/gen/keys/keys';
import type { ApiKey, KeyQuota } from '../../shared/api/gen/models';
import { loadKeys } from '../../features/keys/api';

export type KeyState = 'ok' | 'reserve' | 'exhausted' | 'revoked';

export type KeyUsage = {
  key_id: number;
  used: number;
  person_used: number;
  sizes_used: number;
  reserve: number;
  window_resets_at: string;
  state: KeyState;
};

export type UsageSnapshot = { server_time: string; items: KeyUsage[] };

const failure = (what: string, status: number) =>
  Object.assign(new Error(`Could not ${what}`), { status });

/** Cooling and exhausted keys both wait for the window to end, so the card shows them alike. */
function usageOf(quota: KeyQuota, serverTime: string): KeyUsage {
  return {
    key_id: quota.key_id,
    used: quota.used,
    person_used: quota.lanes.person,
    sizes_used: quota.lanes.photo,
    reserve: quota.reserve,
    window_resets_at: quota.reset_at ?? serverTime,
    state: quota.state === 'ok' ? 'ok' : 'exhausted',
  };
}

/**
 * Reads the usage of the current user's keys: `GET /keys`, then `GET /keys/{id}/quota`
 * for each key that is not revoked. A key whose quota cannot be read is left out.
 */
export async function loadUsage(signal?: AbortSignal): Promise<UsageSnapshot> {
  const keys: ApiKey[] = await loadKeys(signal);
  const serverTime = new Date().toISOString();
  const answers = await Promise.all(
    keys
      .filter((key) => key.status !== 'revoked')
      .map(async (key) => {
        const response = await getKeyQuota(key.id, { signal });
        if (response.status === 200) return usageOf(response.data, serverTime);
        if (response.status === 404) return null;
        throw failure('load usage', response.status);
      }),
  );
  return { server_time: serverTime, items: answers.filter((item) => item !== null) };
}

/** Removes a key and its accounts (`DELETE /keys/{id}`); an already missing key counts as removed. */
export async function removeKey(id: number): Promise<void> {
  const response = await deleteKey(id);
  if (response.status !== 204 && response.status !== 404)
    throw failure('remove the key', response.status);
}

/** Starts the Flickr authorization for a key and returns the page to open. */
export async function startConnect(keyId: number): Promise<string> {
  const response = await connectFlickrAccount({ key_id: keyId });
  if (response.status !== 200) throw failure('start the connection', response.status);
  return response.data.authorize_url;
}
