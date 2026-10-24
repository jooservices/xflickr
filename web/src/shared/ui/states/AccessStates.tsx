import type { ReactNode } from 'react';
import { useI18n } from '../../i18n';
import { Icon } from '../icon';
import type { IconName } from '../icon';
import styles from './States.module.css';

type PageStateBlockProps = {
  testId: string;
  icon: IconName;
  title: string;
  text: string;
  actions: ReactNode;
};

function PageStateBlock({ testId, icon, title, text, actions }: PageStateBlockProps) {
  return (
    <div className={styles.block} data-testid={testId}>
      <span className={styles.icon}>
        <Icon name={icon} size={36} />
      </span>
      <h1 tabIndex={-1}>{title}</h1>
      <p>{text}</p>
      <div className={styles.actions}>{actions}</div>
    </div>
  );
}

type ForbiddenStateProps = {
  testId: string;
  ctaTestId: string;
  /** Called by the Go to home button. */
  onHome: () => void;
};

/** 403 page. It never reveals whether the resource exists. */
export function ForbiddenState({ testId, ctaTestId, onHome }: ForbiddenStateProps) {
  const { t } = useI18n();

  return (
    <PageStateBlock
      testId={testId}
      icon="lock"
      title={t('shell.forbidden.title')}
      text={t('shell.forbidden.text')}
      actions={
        <button
          type="button"
          className={`${styles.button} ${styles.primary}`}
          onClick={onHome}
          data-testid={ctaTestId}
        >
          {t('shell.forbidden.home')}
        </button>
      }
    />
  );
}

type NotFoundStateProps = {
  testId: string;
  onHome: () => void;
  onBack: () => void;
};

/** 404 page with a way home and a way back. */
export function NotFoundState({ testId, onHome, onBack }: NotFoundStateProps) {
  const { t } = useI18n();

  return (
    <PageStateBlock
      testId={testId}
      icon="search"
      title={t('shell.notfound.title')}
      text={t('shell.notfound.text')}
      actions={
        <>
          <button
            type="button"
            className={`${styles.button} ${styles.primary}`}
            onClick={onHome}
            data-testid={`${testId}-home`}
          >
            {t('shell.notfound.home')}
          </button>
          <button
            type="button"
            className={styles.button}
            onClick={onBack}
            data-testid={`${testId}-back`}
          >
            {t('shell.notfound.back')}
          </button>
        </>
      }
    />
  );
}
