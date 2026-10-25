import { useEffect, useState } from 'react';
import type { Translate } from '../../shared/i18n/I18nProvider';

/** Current time in ms, refreshed every second; the local timer behind every countdown. */
export function useNow(active = true, tickMs = 1000): number {
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    if (!active) return undefined;
    const timer = window.setInterval(() => {
      setNow(Date.now());
    }, tickMs);
    return () => {
      window.clearInterval(timer);
    };
  }, [active, tickMs]);

  return now;
}

export const DASH = '–';

/** `~3 h 20 min`, `< 1 min`, or an en dash when nothing waits or there is no capacity. */
export function formatEta(hours: number | null, t: Translate): string {
  if (hours === null || hours <= 0) return DASH;
  const minutes = Math.round(hours * 60);
  if (minutes < 1) return t('user.crawl.eta.lt-min');
  if (minutes < 60) return t('user.crawl.eta.min', { min: minutes });
  const h = Math.floor(minutes / 60);
  const min = minutes % 60;
  return min === 0 ? t('user.crawl.eta.h', { h }) : t('user.crawl.eta.h-min', { h, min });
}

export function formatClock(ms: number, locale: string): string {
  return new Intl.DateTimeFormat(locale, { timeStyle: 'medium' }).format(new Date(ms));
}
