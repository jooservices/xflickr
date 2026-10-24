import { useQuery } from '@tanstack/react-query';
import { useCallback, useMemo } from 'react';
import type { ReactNode } from 'react';
import { getMe } from '../../shared/api/gen/default/default';
import { SessionContext } from './model';
import type { Session, SessionValue } from './model';

export const SESSION_QUERY_KEY = ['session', 'me'] as const;

/** GET /me once; 401 means "no session", any other failure is an error to retry. */
export async function fetchSession(signal?: AbortSignal): Promise<Session> {
  const response = await getMe({ signal });
  if (response.status === 200) {
    const { id, email, role, locale } = response.data;
    return { status: 'authenticated', user: { id, email, role, locale } };
  }
  if (response.status === 401) {
    return { status: 'unauthenticated' };
  }
  // The generated type only lists 200 and 401; a 5xx still arrives here at runtime.
  throw new Error(`GET /me answered ${String((response as { status: number }).status)}`);
}

export function SessionProvider({ children }: { children: ReactNode }) {
  const query = useQuery({
    queryKey: SESSION_QUERY_KEY,
    queryFn: ({ signal }) => fetchSession(signal),
    retry: false,
    staleTime: Infinity,
  });
  const { refetch } = query;

  const retry = useCallback(() => {
    void refetch();
  }, [refetch]);

  const value = useMemo<SessionValue>(() => {
    let session: Session = { status: 'loading' };
    if (query.data !== undefined) session = query.data;
    else if (query.isError) session = { status: 'error' };
    return { session, retry };
  }, [query.data, query.isError, retry]);

  return <SessionContext.Provider value={value}>{children}</SessionContext.Provider>;
}
