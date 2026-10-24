import { useEffect, useState } from 'react';

/** Milliseconds since `updatedAt` (epoch ms; 0 means never), refreshed every `tickMs`. */
export function useAge(updatedAt: number, tickMs = 5000): number {
  const [now, setNow] = useState(() => updatedAt);

  useEffect(() => {
    const sync = () => {
      setNow(Date.now());
    };
    sync();
    const timer = window.setInterval(sync, tickMs);
    return () => {
      window.clearInterval(timer);
    };
  }, [updatedAt, tickMs]);

  return updatedAt === 0 ? 0 : Math.max(0, now - updatedAt);
}
