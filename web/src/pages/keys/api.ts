import { request } from '../../features/auth/api';
import { connectFlickrAccount } from '../../shared/api/gen/flickr-accounts/flickr-accounts';

export type KeyState = 'ok' | 'reserve' | 'exhausted' | 'revoked';

export type KeyUsage = {
  key_id: number;
  used: number;
  person_used: number;
  sizes_used: number;
  reserve: number;
  burst_now: number;
  window_resets_at: string;
  hourly: number[];
  node_id: string;
  node_ip: string;
  paused: boolean;
  state: KeyState;
};

export type UsageSnapshot = { server_time: string; items: KeyUsage[] };

const failure = (what: string, status: number) =>
  Object.assign(new Error(`Could not ${what}`), { status });

/**
 * The calls below are contract requests (docs/ui/views/user-keys.md, Data and API):
 * their paths are not in OpenAPI yet, so they go through the shared request helper
 * (CSRF header, session-expiry signal). A missing endpoint is a failure like any other.
 */

/** Reads the usage snapshot of the current user's keys (`GET /keys/usage`). */
export async function loadUsage(signal?: AbortSignal): Promise<UsageSnapshot> {
  const response = await request<UsageSnapshot>('/keys/usage', 'GET', undefined, signal);
  if (response.status !== 200) throw failure('load usage', response.status);
  return response.data;
}

/** Runs an unsafe call and repeats it once when the CSRF token was rejected. */
async function unsafe(path: string, method: string, body?: unknown) {
  const response = await request<{ code?: string }>(path, method, body);
  if (response.status === 403 && response.data.code === 'csrf_rejected')
    return request<{ code?: string }>(path, method, body);
  return response;
}

/** Pauses or resumes scheduled calls of a key (`PUT /keys/{id}` with `paused`). */
export async function setKeyPaused(id: number, paused: boolean): Promise<void> {
  const response = await unsafe(`/keys/${String(id)}`, 'PUT', { paused });
  if (response.status !== 200) throw failure('update the key', response.status);
}

/** Removes a key and its accounts (`DELETE /keys/{id}`); an already missing key counts as removed. */
export async function removeKey(id: number): Promise<void> {
  const response = await unsafe(`/keys/${String(id)}`, 'DELETE');
  if (response.status !== 204 && response.status !== 404)
    throw failure('remove the key', response.status);
}

/** Starts the Flickr authorization for a key and returns the page to open. */
export async function startConnect(keyId: number): Promise<string> {
  const response = await connectFlickrAccount({ key_id: keyId });
  if (response.status !== 200) throw failure('start the connection', response.status);
  return response.data.authorize_url;
}
