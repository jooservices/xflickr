import type { ReactNode } from 'react';
import { Icon } from '../icon';
import type { IconName } from '../icon';
import { useI18n } from '../../i18n';
import styles from './States.module.css';

type EmptyStateProps = {
  testId: string;
  icon?: IconName;
  title?: string;
  text?: string;
  /** Optional call to action, rendered below the text. */
  action?: ReactNode;
};

/** Empty state: icon, title, text and an optional call to action, in that order. */
export function EmptyState({ testId, icon = 'image', title, text, action }: EmptyStateProps) {
  const { t } = useI18n();

  return (
    <div className={styles.block} data-testid={`${testId}-empty`}>
      <span className={styles.icon}>
        <Icon name={icon} size={30} />
      </span>
      <h2>{title ?? t('common.state.empty.title')}</h2>
      <p>{text ?? t('common.state.empty.text')}</p>
      {action === undefined ? null : <div className={styles.actions}>{action}</div>}
    </div>
  );
}
