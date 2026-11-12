import type { JSX } from 'react';
import { useI18n } from '../../../shared';

type OverviewKpisProps = { exhausted: number | undefined; keyCount: number | undefined };

/** The key numbers; a dash stands for a number that is not known yet. */
export function OverviewKpis({ exhausted, keyCount }: OverviewKpisProps): JSX.Element {
  const { locale, t } = useI18n();
  const number = new Intl.NumberFormat(locale);
  return (
    <section aria-label={t('admin.overview.kpis')} data-testid="admin-overview-kpis">
      <dl>
        <div data-testid="admin-overview-keys-at-quota">
          <dt>{t('admin.overview.keys-at-quota')}</dt>
          <dd>
            {exhausted === undefined ? '—' : number.format(exhausted)}
            {keyCount === undefined ? null : (
              <small>{t('admin.overview.of-keys', { count: number.format(keyCount) })}</small>
            )}
          </dd>
        </div>
      </dl>
    </section>
  );
}
