import { useI18n } from '../../i18n';
import styles from './States.module.css';

type LoadingStateProps = {
  /** Test id prefix: the skeleton is `<testId>-skeleton`. */
  testId: string;
  /** Overrides the `Loading…` line, for example `Loading XFlickr…`. */
  label?: string;
  /** Number of skeleton tiles. */
  tiles?: number;
  /** Hides the skeleton while a fast answer may still arrive (see useLoadingTiming). */
  skeleton?: boolean;
};

/** Loading state: a status line with a spinner, then a skeleton that mirrors the layout. */
export function LoadingState({ testId, label, tiles = 6, skeleton = true }: LoadingStateProps) {
  const { t } = useI18n();

  return (
    <div aria-busy="true" data-testid={`${testId}-loading`}>
      <p className={styles.spinnerLine} role="status">
        <span className={styles.spinner} aria-hidden="true" />
        {label ?? t('common.state.loading')}
      </p>
      {skeleton ? (
        <div className={styles.skeletonGrid} data-testid={`${testId}-skeleton`} aria-hidden="true">
          {Array.from({ length: tiles }, (_, index) => (
            <div key={index} className={styles.skeleton} />
          ))}
        </div>
      ) : null}
    </div>
  );
}
