import { useQuery } from '@tanstack/react-query';
import { useEffect } from 'react';
import type { JSX } from 'react';
import { Link } from 'react-router';
import type { HomeSnapshot } from '../../shared/api/gen/models';
import { getGetMeHomeQueryKey, getMeHome } from '../../shared/api/gen/default/default';
import { EmptyState, ErrorState, LoadingState, useI18n } from '../../shared';

class HomeRequestError extends Error {
  constructor(readonly status: number) {
    super('Home request failed');
    this.name = 'HomeRequestError';
  }
}

async function loadHome(): Promise<HomeSnapshot> {
  const response = await getMeHome();
  if (response.status !== 200) {
    throw new HomeRequestError(response.status);
  }
  const snapshot = response.data.data;
  if (!snapshot?.contacts) throw new HomeRequestError(response.status);
  return snapshot;
}

/** Shows the signed-in user's contact counts from the generated Home API. */
export function HomePage(): JSX.Element {
  const { locale, t } = useI18n();
  const home = useQuery({
    queryKey: getGetMeHomeQueryKey(),
    queryFn: loadHome,
    retry: false,
  });

  useEffect(() => {
    document.title = `${t('shell.user.home.title')} · XFlickr`;
  }, [t]);

  const status = home.error instanceof HomeRequestError ? home.error.status : undefined;
  if (status === 401) return <></>;

  const number = new Intl.NumberFormat(locale);
  const forbidden = status === 403;
  const error = home.isError && !forbidden;

  return (
    <section data-testid="user-home-page">
      <header>
        <h1 data-testid="user-home-title">{t('shell.user.home.title')}</h1>
      </header>
      <section data-testid="user-home-stats" aria-busy={home.isPending}>
        {home.isPending ? (
          <LoadingState testId="user-home" label={t('shell.user.home.status.loading')} tiles={3} />
        ) : null}
        {error ? (
          <>
            <ErrorState
              testId="user-home"
              title={t('shell.user.home.error.title')}
              text={t('shell.user.home.error.text')}
            />
            <button
              type="button"
              data-testid="user-home-error-retry"
              onClick={() => void home.refetch()}
            >
              {t('shell.user.home.error.retry')}
            </button>
          </>
        ) : null}
        {forbidden ? (
          <div data-testid="user-home-forbidden">
            <EmptyState
              testId="user-home-forbidden-state"
              icon="lock"
              title={t('shell.user.home.forbidden.title')}
              text={t('shell.user.home.forbidden.text')}
              action={
                <Link to="/login" data-testid="user-home-forbidden-login">
                  {t('shell.user.home.forbidden.cta')}
                </Link>
              }
            />
          </div>
        ) : null}
        {home.data ? (
          <article data-testid="user-home-stat-contacts">
            <h2>{t('shell.user.home.stats.contacts')}</h2>
            <p>{number.format(home.data.contacts.total)}</p>
            <p data-testid="user-home-stat-direct">
              {t('shell.user.home.stats.direct', {
                count: number.format(home.data.contacts.direct),
              })}
            </p>
            <p data-testid="user-home-stat-discovered">
              {t('shell.user.home.stats.discovered', {
                count: number.format(home.data.contacts.discovered),
              })}
            </p>
          </article>
        ) : null}
      </section>
    </section>
  );
}
