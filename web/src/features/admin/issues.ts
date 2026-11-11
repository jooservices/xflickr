import { useInfiniteQuery } from '@tanstack/react-query';
import { listAdminOpenIncidents } from '../../shared/api/gen/admin/admin';

export const ADMIN_OPEN_INCIDENTS_QUERY_KEY = ['admin', 'issues', 'open'] as const;
const PAGE_SIZE = 50;
const REQUEST_TIMEOUT_MS = 10_000;

async function readOpenIncidents(cursor: string | undefined, signal: AbortSignal) {
  const requestSignal = AbortSignal.any([signal, AbortSignal.timeout(REQUEST_TIMEOUT_MS)]);
  const response = await listAdminOpenIncidents(
    { cursor, limit: PAGE_SIZE },
    { signal: requestSignal },
  );
  if (response.status !== 200) {
    throw Object.assign(new Error('Could not load open issues'), { status: response.status });
  }
  return response.data;
}

/** Loads open incidents through the generated, contract-validated API client. */
export function useAdminOpenIncidents() {
  return useInfiniteQuery({
    queryKey: ADMIN_OPEN_INCIDENTS_QUERY_KEY,
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam, signal }) => readOpenIncidents(pageParam, signal),
    getNextPageParam: (lastPage) => lastPage.next_cursor || undefined,
    retry: false,
    refetchOnWindowFocus: false,
  });
}
