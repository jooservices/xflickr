import { useAdminQuota } from '../../../features/admin/quota';

const STALE_AFTER_MS = 20_000;

/** The numbers of the overview, taken from the quota snapshot that the admin shell already keeps fresh. */
export function useOverviewData(now: number) {
  const quota = useAdminQuota(true);
  const keys = quota.data?.keys;
  const status = (quota.error as { status?: number } | null)?.status;
  return {
    forbidden: status === 403,
    keyCount: keys?.length,
    exhausted: keys?.filter((key) => key.state === 'exhausted').length,
    failed: quota.isError && keys === undefined,
    stale: keys !== undefined && (quota.isError || now - quota.dataUpdatedAt >= STALE_AFTER_MS),
    refreshing: quota.isFetching,
    refetch: () => void quota.refetch(),
  };
}
