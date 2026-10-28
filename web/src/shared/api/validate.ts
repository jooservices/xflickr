import type { ZodType } from 'zod';
import * as contract from './zod/schemas';

export class ApiShapeError extends Error {
  readonly correlationId: string;

  constructor(message = 'The server returned an unexpected response') {
    super(message);
    this.name = 'ApiShapeError';
    this.correlationId = newCorrelationId();
  }
}

export function newCorrelationId(): string {
  return globalThis.crypto?.randomUUID?.() ?? `xf-${Date.now().toString(36)}`;
}

type Route = { method: string; pattern: RegExp; schema: ZodType };

const route = (method: string, path: string, schema: ZodType): Route => ({
  method,
  pattern: new RegExp(`^${path.replace(/\{[^}]+\}/g, '[^/]+')}$`),
  schema,
});

/**
 * Every 2xx body is checked against one of these, the OpenAPI-derived (Orval zod) schemas; `responseSchemas.test.ts` fails when a generated response has no entry.
 */
export const ROUTES: Route[] = [
  route('GET', '/auth/config', contract.GetAuthConfigResponse),
  route('GET', '/healthz', contract.GetHealthResponse),
  route('GET', '/readyz', contract.GetReadinessResponse),
  route('POST', '/auth/register', contract.RegisterResponse),
  route('POST', '/auth/login', contract.LoginResponse),
  route('POST', '/auth/logout', contract.LogoutResponse),
  route('DELETE', '/auth/sessions', contract.RevokeOtherSessionsResponse),
  route('GET', '/me', contract.GetMeResponse),
  route('PATCH', '/me', contract.UpdateMeResponse),
  route('GET', '/me/sessions', contract.ListMySessionsResponse),
  route('DELETE', '/me/sessions/{id}', contract.RevokeMySessionResponse),
  route('POST', '/me/password', contract.ChangeMyPasswordResponse),
  route('PUT', '/me/sharing', contract.PutMeSharingResponse),
  route('GET', '/keys', contract.ListKeysResponse),
  route('POST', '/keys', contract.CreateKeyResponse),
  route('GET', '/keys/{id}', contract.GetKeyResponse),
  route('PUT', '/keys/{id}', contract.UpdateKeyResponse),
  route('DELETE', '/keys/{id}', contract.DeleteKeyResponse),
  route('GET', '/keys/{id}/quota', contract.GetKeyQuotaResponse),
  route('POST', '/flickr-accounts/connect', contract.ConnectFlickrAccountResponse),
  route('GET', '/flickr-accounts/callback', contract.FlickrAccountCallbackResponse),
  route('GET', '/admin/users', contract.ListAdminUsersResponse),
  route('PATCH', '/admin/users/{id}', contract.UpdateAdminUserResponse),
  route('GET', '/admin/plans', contract.GetAdminPlansResponse),
  route('PUT', '/admin/plans', contract.UpdateAdminPlansResponse),
  route('PUT', '/admin/users/{id}/plan', contract.UpdateAdminUserPlanResponse),
  route('GET', '/flickr-accounts', contract.ListFlickrAccountsResponse),
  route('GET', '/me/crawl', contract.GetMeCrawlResponse),
  route('GET', '/me/crawl/queue', contract.GetMeCrawlQueueResponse),
  route('GET', '/me/crawl/settings', contract.GetMeCrawlSettingsResponse),
  route('PUT', '/me/crawl/settings', contract.UpdateMeCrawlSettingsResponse),
  route('PUT', '/me/contacts/{nsid}/star', contract.StarMyContactResponse),
  route('PUT', '/me/contacts/{nsid}/select', contract.SelectMyContactResponse),
  route('PUT', '/me/crawl/pause', contract.PauseCrawlContactResponse),
  route('PUT', '/me/crawl/resume', contract.ResumeCrawlContactResponse),
  route('GET', '/admin/queue', contract.GetAdminQueueResponse),
  route('GET', '/admin/quota', contract.GetAdminQuotaResponse),
  route('GET', '/admin/ops/status', contract.GetAdminOpsStatusResponse),
  route('POST', '/downloads', contract.CreateDownloadResponse),
  route('GET', '/events', contract.StreamEventsResponse),
];

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === 'object' && value !== null && !Array.isArray(value);

function pathnameOf(path: string): string {
  return new URL(path, globalThis.location?.origin ?? 'http://localhost').pathname;
}

/**
 * Checks a response body and returns it. Bodies of 2xx answers must match the schema of their
 * operation; a path without a schema must at least be a JSON object. Anything else is an
 * `ApiShapeError`, never data. Error statuses keep their HTTP handling and only need an object.
 */
export function validateBody(path: string, method: string, status: number, data: unknown): unknown {
  if (status >= 400) {
    if (data === undefined || isRecord(data)) return data;
    throw new ApiShapeError('The server returned an unexpected error body');
  }
  const verb = method.toUpperCase();
  const pathname = pathnameOf(path);
  const found = ROUTES.find((r) => r.method === verb && r.pattern.test(pathname));
  if (status === 204 || status === 205 || status === 304) {
    if (data === undefined) return data;
    throw new ApiShapeError('The server returned a body where none was expected');
  }
  if (data === undefined && found?.schema.safeParse(undefined).success === true) return data;
  if (data === null || data === undefined) {
    throw new ApiShapeError(`${verb} ${pathname} returned an empty body`);
  }
  if (!found) {
    if (isRecord(data)) return data;
    throw new ApiShapeError(`${verb} ${pathname} did not return a JSON object`);
  }
  const result = found.schema.safeParse(data);
  if (!result.success) {
    throw new ApiShapeError(`${verb} ${pathname} did not match the API contract`);
  }
  return data;
}
