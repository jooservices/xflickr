import { createKey, listKeys } from '../../shared/api/gen/keys/keys';
import type { CreateKeyRequest } from '../../shared/api/gen/models';

export async function loadKeys(signal?: AbortSignal) {
  const response = await listKeys({ signal });
  if (response.status !== 200)
    throw Object.assign(new Error('Could not load keys'), { status: response.status });
  return response.data;
}

export async function checkAndSaveKey(request: CreateKeyRequest) {
  return createKey(request);
}
