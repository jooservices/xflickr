import { getMeCrawlSettings, updateMeCrawlSettings } from '../../shared/api/gen/crawl/crawl';
import { listFlickrAccounts } from '../../shared/api/gen/flickr-accounts/flickr-accounts';
import { createKey, listKeys } from '../../shared/api/gen/keys/keys';
import type { ApiKey, CreateKeyRequest, FlickrAccount } from '../../shared/api/gen/models';

export async function loadKeys(signal?: AbortSignal) {
  const response = await listKeys({ signal });
  if (response.status !== 200)
    throw Object.assign(new Error('Could not load keys'), { status: response.status });
  return response.data;
}

export async function checkAndSaveKey(body: CreateKeyRequest) {
  return createKey(body);
}

export type CrawlSettings = { depth: number; depth_default: number; depth_max: number };

/** Lists the current user's Flickr accounts (`GET /flickr-accounts`). */
export async function loadAccounts(signal?: AbortSignal): Promise<FlickrAccount[]> {
  const response = await listFlickrAccounts({ signal });
  if (response.status !== 200)
    throw Object.assign(new Error('Could not load accounts'), { status: response.status });
  return response.data;
}

/** Reads the person's crawl depth and the administrator limits (`GET /me/crawl/settings`). */
export async function loadCrawlSettings(signal?: AbortSignal): Promise<CrawlSettings> {
  const response = await getMeCrawlSettings({ signal });
  if (response.status !== 200)
    throw Object.assign(new Error('Could not load crawl settings'), { status: response.status });
  return response.data;
}

/**
 * Stores the depth the crawl starts from (`PUT /me/crawl/settings`). The crawl itself began when
 * the first account was connected; the depth bounds how far it goes.
 */
export async function startCrawl(depth: number): Promise<{ status: number; code?: string }> {
  const response = await updateMeCrawlSettings({ depth });
  return {
    status: response.status,
    code: 'code' in response.data ? response.data.code : undefined,
  };
}

export type KeyFields = { key?: 'format' | 'duplicate'; secret?: 'format' };
export type KeyFailure = { banner?: 'invalid' | 'network' | 'node'; fields?: KeyFields };
export type KeyResult = { key: ApiKey } | { failure: KeyFailure };

/** Client rules mirror the POC: key 32 hex, secret 16 hex, spaces and line breaks trimmed. */
export const keyPattern = /^[a-f\d]{32}$/i;
export const secretPattern = /^[a-f\d]{16}$/i;
export const clean = (value: string) => value.replace(/\s/g, '');

/** Sends the key for the server to check with Flickr and maps each answer to a form failure. */
export async function saveKey(body: CreateKeyRequest): Promise<KeyResult> {
  try {
    const response = await checkAndSaveKey(body);
    if (response.status === 201) return { key: response.data };
    if (response.status === 400) {
      const fields = (response.data.details as { fields?: Record<string, unknown> } | undefined)
        ?.fields;
      const next: KeyFields = {
        ...(fields && 'key' in fields ? { key: 'format' as const } : {}),
        ...(fields && 'secret' in fields ? { secret: 'format' as const } : {}),
      };
      return next.key || next.secret
        ? { failure: { fields: next } }
        : { failure: { banner: 'network' } };
    }
    if (response.status === 409) return { failure: { fields: { key: 'duplicate' } } };
    if (response.status === 422 && response.data.code === 'key_invalid')
      return { failure: { banner: 'invalid' } };
    if (response.status === 503) return { failure: { banner: 'node' } };
    return { failure: { banner: 'network' } };
  } catch {
    return { failure: { banner: 'network' } };
  }
}
