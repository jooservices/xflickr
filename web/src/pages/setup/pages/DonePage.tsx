import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import type { RefObject } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useNavigate } from 'react-router';
import type { ApiKey, FlickrAccount } from '../../../shared/api/gen/models';
import { useI18n } from '../../../shared';
import { AddKeyModal } from '../../../features/keys/AddKeyModal';
import { loadCrawlSettings, startCrawl } from '../../../features/keys/api';
import { DEFAULT_DEPTH, POLL_MS } from '../setupModel';
import { useSetupData } from '../useSetupData';
import type { SetupPageComponentProps } from '../types';
import { DoneContents, EmptyAccounts } from './DoneContents';

type ConnectedReturn = { connectedId: number | null; connectedMarker: boolean };

/** Owns account polling, crawl settings and starting the crawl. */
export function DonePage({ report, payload, announce, rollback }: SetupPageComponentProps) {
  const { t } = useI18n();
  const data = useSetupData();
  const settings = useDoneSettings(data.hasAccount);
  const actions = useDoneActions({ data, report, announce, settings, t });
  const rows = useRef(new Map<number, HTMLLIElement>());
  const connectedAccount = findConnectedAccount(data.accounts, readConnectedReturn(payload));
  useAccountPolling(data.refetchAccounts);
  useConnectedAnnouncement(connectedAccount, rows, announce, t);

  return (
    <>
      {data.hasAccount ? (
        <DoneContents
          data={data}
          settings={settings}
          actions={actions}
          rollback={rollback}
          rows={rows}
          focusAccountId={connectedAccount?.id}
          showSettingsWarning={!data.staleAccounts}
        />
      ) : (
        <EmptyAccounts active={Boolean(data.activeKey)} report={report} />
      )}
      {actions.addKeyOpen ? (
        <AddKeyModal onAdded={actions.addKey} onClose={() => actions.setAddKeyOpen(false)} />
      ) : null}
    </>
  );
}

function useDoneSettings(enabled: boolean) {
  const query = useQuery({
    queryKey: ['crawl', 'settings'],
    queryFn: ({ signal }) => loadCrawlSettings(signal),
    enabled,
    retry: false,
  });
  const [depthChoice, setDepthChoice] = useState<number | null>(null);
  const depthMax = query.data ? Math.max(1, query.data.depth_max) : DEFAULT_DEPTH;
  const depthStart = Math.min(query.data?.depth ?? DEFAULT_DEPTH, depthMax);
  const depth = Math.min(Math.max(depthChoice ?? depthStart, 1), depthMax);
  return { query, depth, depthMax, setDepthChoice };
}

function useDoneActions({
  data,
  report,
  announce,
  settings,
  t,
}: {
  data: ReturnType<typeof useSetupData>;
  report: SetupPageComponentProps['report'];
  announce: SetupPageComponentProps['announce'];
  settings: ReturnType<typeof useDoneSettings>;
  t: ReturnType<typeof useI18n>['t'];
}) {
  const [addKeyOpen, setAddKeyOpen] = useState(false);
  const crawl = useStartCrawl(settings, report);
  const addKey = (key: ApiKey) => {
    data.addKey(key);
    setAddKeyOpen(false);
    announce(t('setup.addkey.toast'));
    report({ type: 'rollback', target: 'connect' });
  };
  return {
    ...crawl,
    addKeyOpen,
    setAddKeyOpen,
    addKey,
    goToConnect: () => report({ type: 'rollback', target: 'connect' }),
  };
}

function useStartCrawl(
  settings: ReturnType<typeof useDoneSettings>,
  report: SetupPageComponentProps['report'],
) {
  const navigate = useNavigate();
  const [finishError, setFinishError] = useState<'finish' | 'depth' | null>(null);
  const [starting, setStarting] = useState(false);
  const finishing = useRef(false);
  const fail = (error: 'finish' | 'depth') => {
    setFinishError(error);
    report({ type: 'failure', error });
  };
  const finish = async (depth: number) => {
    if (finishing.current) return;
    finishing.current = true;
    setStarting(true);
    setFinishError(null);
    try {
      await startCrawlRequest(depth, settings, navigate, fail);
    } finally {
      finishing.current = false;
      setStarting(false);
    }
  };
  return { finishError, starting, finish };
}

async function startCrawlRequest(
  depth: number,
  settings: ReturnType<typeof useDoneSettings>,
  navigate: ReturnType<typeof useNavigate>,
  fail: (error: 'finish' | 'depth') => void,
) {
  try {
    const result = await startCrawl(depth);
    if (result.status === 200) {
      void navigate('/', { state: { toast: 'setup.done.toast' } });
      return;
    }
    if (result.status === 422 && result.code === 'depth_above_cap') {
      settings.setDepthChoice(null);
      void settings.query.refetch();
      fail('depth');
      return;
    }
    fail('finish');
  } catch {
    fail('finish');
  }
}

function useAccountPolling(refetchAccounts: ReturnType<typeof useSetupData>['refetchAccounts']) {
  useEffect(() => {
    let failures = 0;
    let stopped = false;
    let timer = 0;
    const schedule = () => {
      timer = window.setTimeout(() => void tick(), POLL_MS[Math.min(failures, POLL_MS.length - 1)]);
    };
    const tick = async () => {
      if (!document.hidden) {
        const result = await refetchAccounts();
        failures = result.isError ? failures + 1 : 0;
      }
      if (!stopped) schedule();
    };
    schedule();
    return () => {
      stopped = true;
      window.clearTimeout(timer);
    };
  }, [refetchAccounts]);
}

function useConnectedAnnouncement(
  account: FlickrAccount | undefined,
  rows: RefObject<Map<number, HTMLLIElement>>,
  announce: SetupPageComponentProps['announce'],
  t: ReturnType<typeof useI18n>['t'],
) {
  const announced = useRef<number | null>(null);
  useLayoutEffect(() => {
    if (!account || announced.current === account.id) return;
    announced.current = account.id;
    const timer = window.setTimeout(() => rows.current.get(account.id)?.focus(), 0);
    announce(`${account.username} · ${t('setup.done.account.ok')}`);
    return () => window.clearTimeout(timer);
  }, [account, announce, rows, t]);
}

function findConnectedAccount(
  accounts: FlickrAccount[],
  result: ConnectedReturn | null,
): FlickrAccount | undefined {
  if (!result) return undefined;
  return result.connectedMarker
    ? accounts.at(-1)
    : accounts.find((account) => account.id === result.connectedId);
}

function readConnectedReturn(payload: unknown): ConnectedReturn | null {
  if (!payload || typeof payload !== 'object') return null;
  const result = payload as Partial<ConnectedReturn>;
  if (typeof result.connectedMarker !== 'boolean') return null;
  return { connectedId: result.connectedId ?? null, connectedMarker: result.connectedMarker };
}
