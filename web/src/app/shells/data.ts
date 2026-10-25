import { useQuery } from '@tanstack/react-query';
import { useMemo } from 'react';

import { getAdminQuota } from '../../shared/api/gen/admin/admin';
import { loadAccounts, loadKeys } from '../../features/keys/api';
import type { ApiKey, FlickrAccount } from '../../shared/api/gen/models';

/** Chrome data (docs/ui/views/*-shell.md), built from the contract endpoints the backend serves. */

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

function unavailable(what: string, status: number): Error {
  return Object.assign(new Error(`Could not load ${what}`), { status });
}

/**
 * Badges and banners, assembled from the contract data the pages already load: the person's keys
 * and Flickr accounts give the attention banners. Notifications and downloads have no endpoint
 * yet, so their counts are zero. A failed list only hides its banners.
 */
export function summarize(keys: ApiKey[] | undefined, accounts: FlickrAccount[] | undefined) {
  const attention: Attention[] = [
    ...(keys ?? [])
      .filter((key) => key.status !== 'valid')
      .map((key): Attention => ({ kind: 'key_revoked', id: String(key.id), label: key.label })),
    ...(accounts ?? [])
      .filter((account) => account.status === 'reconnect_required')
      .map((account): Attention => ({
        kind: 'token_expired',
        id: String(account.key_id),
        label: account.username,
      })),
  ];
  const summary: ShellSummary = {
    unread_notifications: 0,
    running_downloads: 0,
    tray_count: 0,
    attention,
  };
  return summary;
}

/** Cluster footer and badges of the administrator shell, derived from the quota overview. */
export async function loadAdminShell(signal?: AbortSignal): Promise<AdminShellData> {
  const response = await getAdminQuota({ signal });
  if (response.status !== 200) throw unavailable('the cluster state', response.status);
  const keys = response.data.keys;
  const nodes = new Map<string, boolean>();
  for (const key of keys) nodes.set(key.node, (nodes.get(key.node) ?? false) || key.state === 'ok');
  const healthy = [...nodes.values()].filter(Boolean).length;
  const status: AdminShellData['cluster']['status'] =
    nodes.size === 0
      ? 'unknown'
      : healthy === nodes.size
        ? 'ok'
        : healthy === 0
          ? 'down'
          : 'degraded';
  return {
    open_issues: keys.filter((key) => key.state !== 'ok').length,
    unread_notifications: 0,
    cluster: {
      status,
      nodes_total: nodes.size,
      nodes_healthy: healthy,
      calls_per_second: keys.reduce((sum, key) => sum + key.used, 0) / 3600,
      calls_history: [],
    },
    generated_at: new Date().toISOString(),
  };
}

export const REQUEST_TIMEOUT_MS = 10_000;

function withTimeout(signal: AbortSignal): AbortSignal {
  return AbortSignal.any([signal, AbortSignal.timeout(REQUEST_TIMEOUT_MS)]);
}

/** The query keys are the ones the keys and wizard pages use, so the shell adds no requests. */
export function useShellSummary(enabled: boolean) {
  const keys = useQuery({
    queryKey: ['/keys'],
    queryFn: ({ signal }) => loadKeys(signal),
    enabled,
    retry: false,
  });
  const accounts = useQuery({
    queryKey: ['/flickr-accounts'],
    queryFn: ({ signal }) => loadAccounts(signal),
    enabled,
    retry: false,
  });
  const data = useMemo(() => summarize(keys.data, accounts.data), [keys.data, accounts.data]);
  return { data };
}

export function useAdminShell(enabled: boolean) {
  return useQuery({
    queryKey: ['admin', 'shell'],
    queryFn: ({ signal }) => loadAdminShell(withTimeout(signal)),
    enabled,
    retry: false,
    refetchInterval: 10_000,
  });
}
