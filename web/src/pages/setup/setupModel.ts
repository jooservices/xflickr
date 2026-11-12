import type { FlickrAccount } from '../../shared/api/gen/models';
import type { MessageKey } from '../../shared/i18n';

export type Step = 1 | 2 | 3;
export type KeyBanner = 'invalid' | 'network' | 'node';
export type FieldError = 'format' | 'duplicate';
export type ConnectError = 'denied' | 'expired' | 'start';

export const SKELETON_DELAY = 150;
/** Polling on step 3 while the live stream is not available: 10, 20, 40, 60 s. */
export const POLL_MS = [10_000, 20_000, 40_000, 60_000];
export const PALETTE = [
  '#e9a97c,#8fd0a4',
  '#7ca8e9,#7cc9d0',
  '#8fd0a4,#7ca8e9',
  '#d98fb4,#9a8fe0',
  '#f0d27a,#e9a97c',
  '#9a8fe0,#f0d27a',
  '#7cc9d0,#b4c97c',
  '#e07c7c,#d98fb4',
  '#b4c97c,#e07c7c',
];

/** Background of mosaic tile `index`; a neutral tile when the palette is empty. */
export function tileBackground(palette: readonly string[], index: number): string {
  if (palette.length === 0) return 'var(--sunken)';
  return `linear-gradient(135deg,${palette[index % palette.length]})`;
}

/** The demo build (and dev server) offers Fill example; production does not. */
export const DEMO = import.meta.env.DEV || import.meta.env.VITE_DEMO === 'true';
export const DEFAULT_DEPTH = 2;

export function randomHex(length: number): string {
  const bytes = crypto.getRandomValues(new Uint8Array(length / 2));
  return Array.from(bytes, (byte) => byte.toString(16).padStart(2, '0')).join('');
}

export const statusOf = (error: unknown) => (error as { status?: number } | null)?.status;

export const accountStatusCopy: Record<FlickrAccount['status'], MessageKey> = {
  ok: 'setup.done.account.ok',
  reconnect_required: 'shell.banner.reconnect',
  revoked: 'setup.done.account.revoked',
};

export const connectErrorCopy: Record<ConnectError, MessageKey> = {
  denied: 'setup.connect.denied',
  expired: 'setup.connect.expired',
  start: 'setup.connect.error.start',
};

export const bannerCopy: Record<KeyBanner, MessageKey> = {
  invalid: 'setup.key.error.invalid',
  network: 'setup.key.error.network',
  node: 'setup.key.error.no-node',
};
