import { useEffect } from 'react';
import type { JSX } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from 'react-router';
import { ForbiddenState, useI18n } from '../../../shared';
import type { MessageKey } from '../../../shared';
import { getAdminPlans } from '../../../shared/api/gen/admin/admin';
import type { Plan } from '../../../shared/api/gen/models';
import styles from '../quota/QuotaPage.module.css';

const queryKey = ['admin', 'plans'] as const;
const columns = [
  ['name', 'admin.defaults.plans'],
  ['weight', 'admin.defaults.weight'],
  ['max_depth_cap', 'admin.defaults.depth'],
  ['max_contacts', 'admin.defaults.contacts'],
  ['ai_quota_per_day', 'admin.defaults.ai'],
  ['zip_ttl_hours', 'admin.defaults.zip'],
  ['resource_share', 'admin.defaults.share'],
] as const satisfies readonly (readonly [keyof Plan, MessageKey])[];

async function loadPlans(signal?: AbortSignal): Promise<Plan[]> {
  const response = await getAdminPlans({ signal });
  if (response.status !== 200) {
    throw Object.assign(new Error('Could not load plans'), { status: response.status });
  }
  return response.data;
}

function PlanRows({ plans }: { plans: Plan[] }): JSX.Element {
  const { t, locale } = useI18n();
  const number = new Intl.NumberFormat(locale);
  const percent = new Intl.NumberFormat(locale, { style: 'percent', maximumFractionDigits: 2 });
  return (
    <div className={styles.tableWrap} data-testid="admin-defaults-plans">
      <table>
        <caption>{t('admin.defaults.plans')}</caption>
        <thead>
          <tr>
            {columns.map(([field, label]) => (
              <th key={field} scope="col">
                {t(label)}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {plans.map((plan) => (
            <tr key={plan.id} data-testid={`admin-defaults-plan-${plan.name}`}>
              {columns.map(([field, label]) => (
                <td key={field} data-label={t(label)}>
                  {field === 'name'
                    ? plan.name
                    : field === 'resource_share'
                      ? percent.format(plan[field])
                      : number.format(plan[field])}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

export function DefaultsPage(): JSX.Element {
  const { t } = useI18n();
  const client = useQueryClient();
  const navigate = useNavigate();
  const query = useQuery({
    queryKey,
    queryFn: ({ signal }) => loadPlans(signal),
    retry: false,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  });
  const forbidden = (query.error as { status?: number } | null)?.status === 403;
  useEffect(() => {
    document.title = t('shell.document-title', { view: t('admin.defaults.title') });
  }, [t]);
  useEffect(() => {
    if (!forbidden) return;
    void client.cancelQueries({ queryKey, exact: true });
    client.removeQueries({ queryKey, exact: true });
  }, [client, forbidden]);

  return (
    <div className={styles.page} data-testid="admin-defaults-page">
      {forbidden ? (
        <ForbiddenState
          testId="admin-defaults-forbidden"
          ctaTestId="admin-defaults-forbidden-home"
          onHome={() => void navigate('/')}
        />
      ) : (
        <>
          <header className={styles.heading}>
            <div>
              <h1>{t('admin.defaults.title')}</h1>
              <p>{t('admin.defaults.partial')}</p>
            </div>
            <button
              className={styles.refresh}
              type="button"
              data-testid="admin-defaults-refresh"
              disabled={query.isFetching}
              onClick={() => void query.refetch()}
            >
              {t('admin.defaults.refresh')}
            </button>
          </header>
          <section
            className={styles.panel}
            aria-labelledby="plans-heading"
            aria-busy={query.isFetching}
          >
            <div className={styles.panelHeading}>
              <h2 id="plans-heading">{t('admin.defaults.plans')}</h2>
            </div>
            {query.isFetching && (
              <div
                className={styles.loading}
                data-testid="admin-defaults-loading"
                role="status"
                aria-busy="true"
              >
                <p>{t('admin.defaults.loading')}</p>
                {query.data === undefined && <div className={styles.skeleton} aria-hidden="true" />}
              </div>
            )}
            {query.isError && (
              <div
                className={query.data === undefined ? styles.error : styles.stale}
                data-testid={
                  query.data === undefined ? 'admin-defaults-error' : 'admin-defaults-stale'
                }
                role={query.data === undefined ? 'alert' : 'status'}
              >
                <span>
                  {t(query.data === undefined ? 'admin.defaults.error' : 'admin.defaults.stale')}
                </span>
                {query.data === undefined && (
                  <button
                    className={styles.refresh}
                    type="button"
                    onClick={() => void query.refetch()}
                  >
                    {t('admin.defaults.retry')}
                  </button>
                )}
              </div>
            )}
            {query.data !== undefined &&
              (query.data.length === 0 ? (
                <p data-testid="admin-defaults-empty">{t('admin.defaults.empty')}</p>
              ) : (
                <PlanRows plans={query.data} />
              ))}
          </section>
        </>
      )}
    </div>
  );
}
