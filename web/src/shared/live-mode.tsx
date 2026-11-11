import { createContext, useContext } from 'react';
import type { ReactNode } from 'react';

const LiveOffContext = createContext(false);

export function LiveOffProvider({ liveOff, children }: { liveOff: boolean; children: ReactNode }) {
  return <LiveOffContext.Provider value={liveOff}>{children}</LiveOffContext.Provider>;
}

export function useLiveOff(): boolean {
  return useContext(LiveOffContext);
}
