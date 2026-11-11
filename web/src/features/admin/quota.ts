import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useLiveOff } from '../../shared';
import { getAdminQuota } from '../../shared/api/gen/admin/admin';
import type { AdminQuota } from '../../shared/api/gen/models';

export const ADMIN_QUOTA_QUERY_KEY = ['admin', 'quota'] as const;
export const REQUEST_TIMEOUT_MS = 10_000;

const ADMIN_QUOTA_POLL_FAILURES_QUERY_KEY = [...ADMIN_QUOTA_QUERY_KEY, 'poll-failures'] as const;
const POLL_INTERVALS = [10_000, 20_000, 40_000, 60_000];

/** Returns the polling delay for the current consecutive-failure count. */
export function pollIntervalForFailures(consecutiveFailures: number): number {
  return POLL_INTERVALS[Math.min(Math.max(0, consecutiveFailures), POLL_INTERVALS.length - 1)];
}

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
  const queryClient = useQueryClient();
  const liveOff = useLiveOff();
  const query = useQuery({
    queryKey: ADMIN_QUOTA_QUERY_KEY,
    queryFn: async ({ signal }) => {
      try {
        const data = await loadAdminQuota(signal);
        queryClient.setQueryData(ADMIN_QUOTA_POLL_FAILURES_QUERY_KEY, 0);
        return data;
      } catch (error) {
        if (!(error instanceof Error && error.name === 'AbortError')) {
          queryClient.setQueryData<number>(
            ADMIN_QUOTA_POLL_FAILURES_QUERY_KEY,
            (count) => (count ?? 0) + 1,
          );
        }
        throw error;
      }
    },
    enabled: enabled && !liveOff,
    retry: false,
    refetchInterval: (query) => {
      const error = query.state.error as { status?: number } | null;
      return error?.status === 403
        ? false
        : pollIntervalForFailures(
            queryClient.getQueryData<number>(ADMIN_QUOTA_POLL_FAILURES_QUERY_KEY) ?? 0,
          );
    },
    refetchIntervalInBackground: false,
  });

  return query;
}
