import { useEffect, useState } from 'react';
import { Icon, PageState, StatePreview, useI18n } from '../../shared';
import type { IconName, MessageKey, ViewState } from '../../shared';
import styles from './PlaceholderPage.module.css';

type PlaceholderPageProps = {
  /** Catalog key of the page title (the nav label). */
  titleKey: MessageKey;
  /** `shell` for the user app and `admin` for the admin app: prefix of the state test ids. */
  scope: 'shell' | 'admin';
  emptyIcon?: IconName;
  emptyTitleKey?: MessageKey;
  emptyTextKey?: MessageKey;
};

/**
 * Stand-in for a page whose content is built in its own ticket. It already has
 * the shared page states, and in development builds the POC Preview control.
 */
export function PlaceholderPage({
  titleKey,
  scope,
  emptyIcon,
  emptyTitleKey,
  emptyTextKey,
}: PlaceholderPageProps) {
  const { t } = useI18n();
  const [state, setState] = useState<ViewState>('normal');
  const title = t(titleKey);

  useEffect(() => {
    document.title = t('shell.document-title', { view: title });
  }, [t, title]);

  return (
    <div data-testid={`${scope}-page`}>
      <h1 tabIndex={-1}>{title}</h1>
      <StatePreview state={state} onChange={setState} testIdPrefix={`${scope}-preview`} />
      <PageState
        state={state}
        testId={scope}
        onRetry={() => {
          setState('loading');
          window.setTimeout(() => {
            setState('normal');
          }, 600);
        }}
        emptyIcon={emptyIcon}
        emptyTitle={emptyTitleKey === undefined ? undefined : t(emptyTitleKey)}
        emptyText={emptyTextKey === undefined ? undefined : t(emptyTextKey)}
      >
        <div className={styles.placeholder}>
          <Icon name="sparkle" size={24} />
          <p>{t('common.placeholder.text')}</p>
        </div>
      </PageState>
    </div>
  );
}
