import { useEffect } from 'react';
import { useSetupData } from '../useSetupData';
import type { SetupPageComponentProps } from '../types';
import { ConnectView } from './ConnectView';
import { useConnectFlow } from './useConnectFlow';

/** Connects one stored key to Flickr and hands off to the return page. */
export function ConnectPage({
  report,
  failure,
  rollback,
  setRollbackEnabled,
}: SetupPageComponentProps) {
  const data = useSetupData();
  const flow = useConnectFlow({ activeKey: data.activeKey, failure, report });
  useEffect(() => setRollbackEnabled?.(!flow.waiting), [flow.waiting, setRollbackEnabled]);
  return (
    <ConnectView
      activeKey={data.activeKey}
      validKeys={data.validKeys}
      error={flow.error}
      waiting={flow.waiting}
      setSelectedKeyId={data.setSelectedKeyId}
      rollback={rollback}
      authorize={() => void flow.authorize()}
      cancel={flow.cancel}
    />
  );
}
