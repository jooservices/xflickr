import type { ReactNode } from 'react';
import { useI18n } from '../../i18n';
import { EmptyState } from './EmptyState';
import { ErrorState } from './ErrorState';
import { LoadingState } from './LoadingState';
import styles from './States.module.css';
import type { IconName } from '../icon';

export const VIEW_STATES = ['normal', 'loading', 'empty', 'error'] as const;
export type ViewState = (typeof VIEW_STATES)[number];

type PageStateProps = {
  state: ViewState;
  testId: string;
  children: ReactNode;
  onRetry?: () => void;
  emptyIcon?: IconName;
  emptyTitle?: string;
  emptyText?: string;
};

/** Renders the shared loading, empty or error state, or the view itself when normal. */
export function PageState({
  state,
  testId,
  children,
  onRetry,
  emptyIcon,
  emptyTitle,
  emptyText,
}: PageStateProps) {
  switch (state) {
    case 'loading':
      return <LoadingState testId={testId} />;
    case 'empty':
      return <EmptyState testId={testId} icon={emptyIcon} title={emptyTitle} text={emptyText} />;
    case 'error':
      return <ErrorState testId={testId} onRetry={onRetry} withSkeleton />;
    case 'normal':
      return <>{children}</>;
  }
}

type StatePreviewProps = {
  state: ViewState;
  onChange: (state: ViewState) => void;
  testIdPrefix?: string;
};

/**
 * Development-only control (POC "Preview"): switches a view between Normal,
 * Loading, Empty and Error. It renders nothing in production builds.
 */
export function StatePreview({
  state,
  onChange,
  testIdPrefix = 'shell-preview',
}: StatePreviewProps) {
  const { t } = useI18n();

  if (!import.meta.env.DEV) return null;

  return (
    <div className={styles.preview} role="group" aria-label={t('common.preview.label')}>
      <span aria-hidden="true">{t('common.preview.label')}</span>
      {VIEW_STATES.map((item) => (
        <button
          key={item}
          type="button"
          className={styles.previewButton}
          aria-pressed={state === item}
          onClick={() => {
            onChange(item);
          }}
          data-testid={`${testIdPrefix}-${item}`}
        >
          {t(`common.preview.${item}`)}
        </button>
      ))}
    </div>
  );
}
