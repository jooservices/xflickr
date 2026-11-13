import type { ComponentType, ReactNode } from 'react';
import type { MessageKey } from '../../shared/i18n';
import type { SetupData } from './useSetupData';

export type SetupPageId = string;

export type SetupPageEvent =
  | { type: 'success'; payload?: unknown }
  | { type: 'failure'; error?: string }
  | { type: 'rollback'; target: SetupPageId };

export type SetupPageComponentProps = {
  rollback?: ReactNode;
  setRollbackEnabled?: (enabled: boolean) => void;
  report: (event: SetupPageEvent) => void;
  failure: string | null;
  payload: unknown;
  announce: (message: string) => void;
};

export interface SetupPage {
  id: SetupPageId;
  title: (data: SetupData) => MessageKey;
  progressName: MessageKey;
  component: ComponentType<SetupPageComponentProps>;
  next: SetupPageId | null;
  onFail: SetupPageId;
  rollbackTo: SetupPageId[];
  showRollback: (data: SetupData, target: SetupPageId) => boolean;
  visibleInProgress: boolean;
}

export type SetupPageDefinition = Omit<
  SetupPage,
  'title' | 'onFail' | 'rollbackTo' | 'showRollback' | 'visibleInProgress'
> & {
  title: MessageKey | SetupPage['title'];
  onFail?: SetupPageId;
  rollbackTo?: SetupPageId[];
  showRollback?: SetupPage['showRollback'];
  visibleInProgress?: boolean;
};

export type WizardRoute = {
  pageId: SetupPageId | null;
  failure: string | null;
  payload: unknown;
  hostError: string | null;
  finished: boolean;
  revision: number;
};

export type WizardView = 'loading' | 'forbidden' | 'error' | 'steps';
