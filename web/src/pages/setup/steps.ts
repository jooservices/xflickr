import type { SetupData } from './useSetupData';
import { definePage } from './definePage';
import type { SetupPage } from './types';
import { ConnectPage } from './pages/ConnectPage';
import { ConnectResultPage } from './pages/ConnectResultPage';
import { DonePage } from './pages/DonePage';
import { KeyPage } from './pages/KeyPage';

function doneTitle(data: SetupData) {
  return data.hasAccount ? 'setup.done.title' : 'setup.empty.title';
}

export const setupSteps: SetupPage[] = [
  definePage({
    id: 'key',
    title: 'setup.key.title',
    progressName: 'setup.step.key',
    component: KeyPage,
    next: 'connect',
    rollbackTo: ['done'],
    showRollback: (data) => data.hasAccount,
  }),
  definePage({
    id: 'connect',
    title: 'setup.connect.title',
    progressName: 'setup.step.connect',
    component: ConnectPage,
    next: 'connect-result',
    rollbackTo: ['key'],
  }),
  definePage({
    id: 'connect-result',
    title: 'setup.connect.title',
    progressName: 'setup.step.connect',
    component: ConnectResultPage,
    next: 'done',
    onFail: 'connect',
    rollbackTo: [],
    visibleInProgress: false,
  }),
  definePage({
    id: 'done',
    title: doneTitle,
    progressName: 'setup.step.done',
    component: DonePage,
    next: null,
    rollbackTo: ['connect', 'key'],
    showRollback: (data, target) => data.hasAccount && target === 'connect',
  }),
];
