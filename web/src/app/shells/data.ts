import { useQuery } from '@tanstack/react-query';

/**
 * Chrome data. These endpoints are contract requests from the shell specs
 * (docs/ui/views/*-shell.md) and are not in contracts/openapi yet, so the
 * shapes follow the spec exactly and any failure is handled as a partial state.
 */

export type Attention = {
  kind: 'key_revoked' | 'token_expired' | 'cloud_expired';
  id: string;
  label: string;
};

export type ShellSummary = {
  unread_notifications: number;
  running_downloads: number;
  tray_count: number;
  attention: Attention[];
};

export type TrayItem = {
  photo_id: string;
  title: string;
  owner_name: string;
  thumb_url: string;
  added_at: string;
};

export type Tray = { items: TrayItem[]; count: number; estimated_bytes: number };

export type NotificationItem = {
  id: string;
  kind: string;
  tone: 'ok' | 'warn' | 'bad' | 'info';
  text_key: string;
  params: Record<string, string | number>;
  created_at: string;
  read: boolean;
};

export type AdminShellData = {
  open_issues: number;
  unread_notifications: number;
  cluster: {
    status: 'ok' | 'degraded' | 'down' | 'unknown';
    nodes_total: number;
    nodes_healthy: number;
    calls_per_second: number;
    calls_history: number[];
  };
  generated_at: string;
};

export async function fetchJson<T>(url: string, init?: RequestInit): Promise<T> {
  const response = await fetch(url, { credentials: 'same-origin', ...init });
  if (!response.ok) {
    throw new Error(`${url} answered ${String(response.status)}`);
  }
  return (await response.json()) as T;
}

export async function sendRequest(url: string, method: string, body?: unknown): Promise<void> {
  const response = await fetch(url, {
    method,
    credentials: 'same-origin',
    ...(body === undefined
      ? {}
      : { headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) }),
  });
  if (!response.ok) {
    throw new Error(`${method} ${url} answered ${String(response.status)}`);
  }
}

export const REQUEST_TIMEOUT_MS = 10_000;

function withTimeout(signal: AbortSignal): AbortSignal {
  return AbortSignal.any([signal, AbortSignal.timeout(REQUEST_TIMEOUT_MS)]);
}

/** Badges and banners. A failure only hides them; nothing else breaks. */
export function useShellSummary(enabled: boolean) {
  return useQuery({
    queryKey: ['shell', 'summary'],
    queryFn: ({ signal }) =>
      fetchJson<ShellSummary>('/shell/summary', { signal: withTimeout(signal) }),
    enabled,
    retry: false,
  });
}

export function useTray(enabled: boolean) {
  return useQuery({
    queryKey: ['shell', 'tray'],
    queryFn: ({ signal }) => fetchJson<Tray>('/tray', { signal: withTimeout(signal) }),
    enabled,
    retry: false,
  });
}

export function useNotifications(enabled: boolean) {
  return useQuery({
    queryKey: ['shell', 'notifications'],
    queryFn: ({ signal }) =>
      fetchJson<{ items: NotificationItem[] }>('/notifications?limit=30', {
        signal: withTimeout(signal),
      }),
    enabled,
    retry: false,
  });
}

export function useAdminShell(enabled: boolean) {
  return useQuery({
    queryKey: ['admin', 'shell'],
    queryFn: ({ signal }) =>
      fetchJson<AdminShellData>('/admin/shell', { signal: withTimeout(signal) }),
    enabled,
    retry: false,
    refetchInterval: 10_000,
  });
}
