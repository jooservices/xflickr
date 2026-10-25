import { useEffect, useState } from 'react';
import { catalogs } from '../../shared';
import type { MessageKey } from '../../shared';
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

/** `mm:ss` for a remaining time in ms (never negative). */
export function formatCountdown(ms: number): string {
  const total = Math.max(0, Math.ceil(ms / 1000));
  const minutes = Math.floor(total / 60);
  const seconds = total % 60;
  return `${String(minutes).padStart(2, '0')}:${String(seconds).padStart(2, '0')}`;
}

export const NO_COUNTDOWN = '--:--';

/** `3 min ago`, or an en dash when the contact was never checked. */
export function formatAgo(iso: string | null, now: number, t: Translate): string {
  if (iso === null) return DASH;
  const seconds = Math.max(0, Math.round((now - Date.parse(iso)) / 1000));
  if (Number.isNaN(seconds)) return DASH;
  if (seconds < 60) return t('user.crawl.ago.now');
  if (seconds < 3600) return t('user.crawl.ago.min', { n: Math.floor(seconds / 60) });
  if (seconds < 86_400) return t('user.crawl.ago.hour', { n: Math.floor(seconds / 3600) });
  return t('user.crawl.ago.day', { n: Math.floor(seconds / 86_400) });
}

export function formatDateTime(iso: string | null, locale: string): string {
  if (iso === null) return DASH;
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return DASH;
  return new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'medium' }).format(date);
}

export function formatClock(ms: number, locale: string): string {
  return new Intl.DateTimeFormat(locale, { timeStyle: 'medium' }).format(new Date(ms));
}

export function initials(name: string): string {
  return (
    name
      .split(/\s+/)
      .filter(Boolean)
      .slice(0, 2)
      .map((part) => part.charAt(0).toUpperCase())
      .join('') || '?'
  );
}

/** Localised facet name; a facet the catalog does not know is shown as sent. */
export function facetLabel(t: Translate, facet: string): string {
  const key = `user.crawl.facet.${facet}`;
  return key in catalogs.en ? t(key as MessageKey) : facet;
}
