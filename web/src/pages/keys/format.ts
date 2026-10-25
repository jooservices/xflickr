import type { ApiKey } from '../../shared/api/gen/models';
import type { KeyUsage } from './api';

export type Pill = 'ok' | 'reserve' | 'exhausted' | 'revoked' | 'invalid';
export type Hint = 'revoked' | 'exhausted' | 'reserve';

/** The state pill: a key that Flickr refuses wins over the quota state. */
export function pillOf(key: ApiKey, usage: KeyUsage | undefined): Pill | null {
  if (key.status === 'revoked') return 'revoked';
  if (key.status === 'invalid') return 'invalid';
  return usage?.state ?? null;
}

/** The single hint line of a card, most urgent first. */
export function hintOf(pill: Pill | null): Hint | null {
  if (pill === 'revoked' || pill === 'invalid') return 'revoked';
  if (pill === 'exhausted') return 'exhausted';
  if (pill === 'reserve') return 'reserve';
  return null;
}

export const isTerminal = (pill: Pill | null) => pill === 'revoked' || pill === 'invalid';

/** Share of `value` in `cap` as a percentage clamped to 0..100. */
export function percent(value: number, cap: number): number {
  return cap <= 0 ? 0 : Math.min(100, Math.max(0, (value / cap) * 100));
}

/** Milliseconds to the window end, counted from the server clock plus the time since the answer. */
export function msToReset(
  resetsAt: string,
  serverTime: string,
  receivedAt: number,
  now: number,
): number {
  return Date.parse(resetsAt) - (Date.parse(serverTime) + (now - receivedAt));
}

/** mm:ss (or h:mm:ss) for a countdown; `--:--` when it cannot be trusted. */
export function clock(ms: number | null): string {
  if (ms === null || Number.isNaN(ms)) return '--:--';
  const total = Math.max(0, Math.ceil(ms / 1000));
  const hours = Math.floor(total / 3600);
  const minutes = String(Math.floor((total % 3600) / 60)).padStart(2, '0');
  const seconds = String(total % 60).padStart(2, '0');
  return hours > 0 ? `${String(hours)}:${minutes}:${seconds}` : `${minutes}:${seconds}`;
}
