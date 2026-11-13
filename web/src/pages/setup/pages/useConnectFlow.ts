import { useCallback, useEffect, useRef, useState } from 'react';
import type { RefObject } from 'react';
import { connectFlickrAccount } from '../../../shared/api/gen/flickr-accounts/flickr-accounts';
import type { ApiKey } from '../../../shared/api/gen/models';
import type { ConnectError } from '../setupModel';
import type { SetupPageComponentProps } from '../types';

type ConnectFlowArgs = {
  activeKey: ApiKey | null;
  failure: string | null;
  report: SetupPageComponentProps['report'];
};

export function useConnectFlow({ activeKey, failure, report }: ConnectFlowArgs) {
  const [waiting, setWaiting] = useState(false);
  const [localError, setLocalError] = useState<ConnectError | null>(null);
  const [hideFailure, setHideFailure] = useState(false);
  const abort = useRef<AbortController | null>(null);
  const error = localError ?? (hideFailure ? null : connectError(failure));

  useEffect(() => {
    if (failure && !activeKey) report({ type: 'rollback', target: 'key' });
  }, [activeKey, failure, report]);
  useEffect(() => () => abort.current?.abort(), []);
  const actions = useConnectActions({
    activeKey,
    waiting,
    abortRef: abort,
    report,
    setWaiting,
    setLocalError,
    setHideFailure,
  });
  return { ...actions, error, waiting };
}

function connectError(value: string | null): ConnectError | null {
  return value === 'denied' || value === 'expired' || value === 'start' ? value : null;
}

function useConnectActions({
  activeKey,
  waiting,
  abortRef,
  report,
  setWaiting,
  setLocalError,
  setHideFailure,
}: {
  activeKey: ApiKey | null;
  waiting: boolean;
  abortRef: RefObject<AbortController | null>;
  report: SetupPageComponentProps['report'];
  setWaiting: (value: boolean) => void;
  setLocalError: (value: ConnectError | null) => void;
  setHideFailure: (value: boolean) => void;
}) {
  const fail = useCallback(() => {
    setLocalError('start');
    setWaiting(false);
    report({ type: 'failure', error: 'start' });
  }, [report, setLocalError, setWaiting]);
  const authorize = useCallback(async () => {
    if (!activeKey || waiting) return;
    const controller = new AbortController();
    abortRef.current = controller;
    setWaiting(true);
    setLocalError(null);
    setHideFailure(true);
    await requestAuthorization(activeKey, controller, report, fail);
  }, [activeKey, abortRef, fail, report, setHideFailure, setLocalError, setWaiting, waiting]);
  const cancel = useCallback(() => {
    abortRef.current?.abort();
    setWaiting(false);
  }, [abortRef, setWaiting]);
  return { authorize, cancel };
}

async function requestAuthorization(
  key: ApiKey,
  controller: AbortController,
  report: SetupPageComponentProps['report'],
  fail: () => void,
) {
  try {
    const response = await connectFlickrAccount({ key_id: key.id }, { signal: controller.signal });
    if (controller.signal.aborted) return;
    if (response.status === 200) {
      report({ type: 'success' });
      window.location.assign(response.data.authorize_url);
    } else fail();
  } catch {
    if (!controller.signal.aborted) fail();
  }
}
