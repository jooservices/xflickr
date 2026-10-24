import { createContext, useContext } from 'react';

export type SessionRole = 'user' | 'admin';

export type SessionUser = {
  id: number;
  email: string;
  role: SessionRole;
  locale: string;
};

/**
 * What the shells know about the viewer.
 * `loading`: GET /me is pending. `unauthenticated`: it answered 401.
 * `error`: it failed for another reason (network, 5xx, timeout).
 */
export type Session =
  | { status: 'loading' }
  | { status: 'unauthenticated' }
  | { status: 'error' }
  | { status: 'authenticated'; user: SessionUser };

export type SessionValue = {
  session: Session;
  /** Repeats GET /me. */
  retry: () => void;
};

export const SessionContext = createContext<SessionValue | null>(null);

export function useSession(): SessionValue {
  const value = useContext(SessionContext);
  if (value === null) {
    throw new Error('useSession must be used inside SessionProvider');
  }
  return value;
}
