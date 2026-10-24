import { useI18n } from '../../i18n';
import { Icon } from '../icon';
import styles from './States.module.css';

type ErrorStateProps = {
  testId: string;
  title?: string;
  text?: string;
  onRetry?: () => void;
  /** Dims the skeleton below the banner, as the pattern asks for full-page errors. */
  withSkeleton?: boolean;
};

/** Error state: a banner with what failed and a Retry button. */
export function ErrorState({
  testId,
  title,
  text,
  onRetry,
  withSkeleton = false,
}: ErrorStateProps) {
  const { t } = useI18n();

  return (
    <div data-testid={`${testId}-error`}>
      <div className={styles.banner} role="alert">
        <div>
          <strong>
            <Icon name="alert" size={16} /> {title ?? t('common.state.error.title')}
          </strong>
          {text === '' ? null : <p>{text ?? t('common.state.error.text')}</p>}
        </div>
        {onRetry === undefined ? null : (
          <button
            type="button"
            className={styles.button}
            onClick={onRetry}
            data-testid={`${testId}-error-retry`}
          >
            {t('common.retry')}
          </button>
        )}
      </div>
      {withSkeleton ? (
        <div className={`${styles.skeletonGrid} ${styles.dimmed}`} aria-hidden="true">
          {Array.from({ length: 4 }, (_, index) => (
            <div key={index} className={styles.skeleton} />
          ))}
        </div>
      ) : null}
    </div>
  );
}
