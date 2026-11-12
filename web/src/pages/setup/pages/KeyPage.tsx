import { useLayoutEffect } from 'react';
import { useI18n } from '../../../shared';
import { KeyStep } from '../KeyStep';
import { useKeyForm } from '../useKeyForm';
import { useSetupData } from '../useSetupData';
import type { SetupPageComponentProps } from '../types';

/** Owns the key form and reports its outcome to the wizard host. */
export function KeyPage({ report, announce, rollback }: SetupPageComponentProps) {
  const { t } = useI18n();
  const { addKey } = useSetupData();
  const keyForm = useKeyForm({
    onSaved: (key) => {
      addKey(key);
      announce(t('setup.key.ok'));
      report({ type: 'success' });
    },
  });

  useLayoutEffect(() => {
    if (keyForm.banner) report({ type: 'failure', error: keyForm.banner });
  }, [keyForm.banner, report]);

  return <KeyStep keyForm={keyForm} rollback={rollback} />;
}
