import { useQuery } from '@tanstack/react-query';
import { getAdminQuota } from '../../shared/api/gen/admin/admin';
import type { AdminQuota } from '../../shared/api/gen/models';

export const ADMIN_QUOTA_QUERY_KEY = ['admin', 'quota'] as const;
export const REQUEST_TIMEOUT_MS = 10_000;

const POLL_INTERVALS = [10_000, 20_000, 40_000, 60_000];

/** Loads the generated ADMIN quota resource and turns HTTP failures into query errors. */
export async function loadAdminQuota(signal?: AbortSignal): Promise<AdminQuota> {
  const requestSignal =
    signal === undefined
      ? AbortSignal.timeout(REQUEST_TIMEOUT_MS)
      : AbortSignal.any([signal, AbortSignal.timeout(REQUEST_TIMEOUT_MS)]);
  const response = await getAdminQuota({ signal: requestSignal });
  if (response.status !== 200) {
    throw Object.assign(new Error('Could not load API quota'), { status: response.status });
  }
  return response.data;
}

/** Shares one quota request between the admin shell and its quota view. */
export function useAdminQuota(enabled: boolean) {
  return useQuery({
    queryKey: ADMIN_QUOTA_QUERY_KEY,
    queryFn: ({ signal }) => loadAdminQuota(signal),
    enabled,
    retry: false,
    refetchInterval: (query) =>
      POLL_INTERVALS[Math.min(query.state.errorUpdateCount, POLL_INTERVALS.length - 1)],
    refetchIntervalInBackground: false,
  });
}
