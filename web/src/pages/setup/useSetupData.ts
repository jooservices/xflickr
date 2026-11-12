import { createContext, createElement, useContext, useState, type ReactNode } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import type { ApiKey, FlickrAccount } from '../../shared/api/gen/models';
import { useLoadingTiming } from '../../shared';
import { loadAccounts, loadKeys } from '../../features/keys/api';
import { SKELETON_DELAY, statusOf } from './setupModel';

export type SetupData = {
  keys: ApiKey[];
  validKeys: ApiKey[];
  accounts: FlickrAccount[];
  activeKey: ApiKey | null;
  selectedKeyId: number | null;
  setSelectedKeyId: (id: number) => void;
  hasAccount: boolean;
  initialLoading: boolean;
  showSkeleton: boolean;
  forbidden: boolean;
  loadFailed: boolean;
  staleAccounts: boolean;
  retryLoad: () => void;
  refetchAccounts: () => Promise<{ isError: boolean }>;
  addKey: (key: ApiKey) => void;
};

const SetupDataContext = createContext<SetupData | null>(null);

/** Loads the account and key data shared by setup pages. */
export function SetupDataProvider({ children }: { children: ReactNode }) {
  const value = useSetupDataSource();
  return createElement(SetupDataContext.Provider, { value }, children);
}

/** Reads shared keys, accounts and their load state inside the setup wizard. */
export function useSetupData(): SetupData {
  const data = useContext(SetupDataContext);
  if (!data) throw new Error('useSetupData must be used inside SetupDataProvider');
  return data;
}

function useSetupDataSource(): SetupData {
  const client = useQueryClient();
  const [selectedKeyId, setSelectedKeyId] = useState<number | null>(null);
  const keysQuery = useQuery({
    queryKey: ['/keys'],
    queryFn: ({ signal }) => loadKeys(signal),
    retry: false,
  });
  const accountsQuery = useQuery({
    queryKey: ['/flickr-accounts'],
    queryFn: ({ signal }) => loadAccounts(signal),
    retry: false,
  });
  const initialLoading = keysQuery.isPending || accountsQuery.isPending;
  const showSkeleton = useLoadingTiming(initialLoading, SKELETON_DELAY);
  return createSetupData({
    client,
    keysQuery,
    accountsQuery,
    selectedKeyId,
    setSelectedKeyId,
    initialLoading,
    showSkeleton,
  });
}

type SetupQuery<T> = {
  data: T | undefined;
  isPending: boolean;
  isError: boolean;
  error: unknown;
  refetch: () => Promise<{ isError: boolean }>;
};

type SetupDataSourceArgs = {
  client: ReturnType<typeof useQueryClient>;
  keysQuery: SetupQuery<ApiKey[]>;
  accountsQuery: SetupQuery<FlickrAccount[]>;
  selectedKeyId: number | null;
  setSelectedKeyId: (id: number | null) => void;
  initialLoading: boolean;
  showSkeleton: boolean;
};

function createSetupData({
  client,
  keysQuery,
  accountsQuery,
  selectedKeyId,
  setSelectedKeyId,
  initialLoading,
  showSkeleton,
}: SetupDataSourceArgs): SetupData {
  const keys = keysQuery.data ?? [];
  const accounts = accountsQuery.data ?? [];
  const validKeys = keys.filter((key) => key.status === 'valid');
  const activeKey = keys.find((key) => key.id === selectedKeyId) ?? validKeys.at(-1) ?? null;
  const forbidden = statusOf(keysQuery.error) === 403 || statusOf(accountsQuery.error) === 403;
  const loadFailed =
    !forbidden && (keysQuery.isError || (accountsQuery.isError && !accountsQuery.data));
  return {
    keys,
    validKeys,
    accounts,
    activeKey,
    selectedKeyId,
    setSelectedKeyId,
    hasAccount: accounts.length > 0,
    initialLoading,
    showSkeleton,
    forbidden,
    loadFailed,
    staleAccounts: accountsQuery.isError && accountsQuery.data !== undefined,
    retryLoad: retryFailedQueries(keysQuery, accountsQuery),
    refetchAccounts: accountsQuery.refetch,
    addKey: createKeyAdder(client, setSelectedKeyId),
  };
}

function createKeyAdder(
  client: ReturnType<typeof useQueryClient>,
  selectKey: (id: number | null) => void,
) {
  return (key: ApiKey) => {
    client.setQueryData<ApiKey[]>(['/keys'], (current) => [...(current ?? []), key]);
    selectKey(key.id);
    void client.invalidateQueries({ queryKey: ['/keys'] });
  };
}

function retryFailedQueries(keys: SetupQuery<ApiKey[]>, accounts: SetupQuery<FlickrAccount[]>) {
  return () => {
    if (keys.isError) void keys.refetch();
    if (accounts.isError) void accounts.refetch();
  };
}
