import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render } from '@testing-library/react';
import { RouterProvider, createMemoryRouter } from 'react-router';
import { routes } from '../app/router';
import { SessionContext } from '../entities';
import type { Session } from '../entities';
import { I18nProvider } from '../shared';
import type { Locale } from '../shared';

export const adminSession: Session = {
  status: 'authenticated',
  user: { id: 1, email: 'root@example.test', role: 'admin', locale: 'en' },
};

export const userSession: Session = {
  status: 'authenticated',
  user: { id: 2, email: 'anna@example.test', role: 'user', locale: 'en' },
};

type Options = {
  session: Session;
  path?: string;
  locale?: Locale;
  retry?: () => void;
};

/** Renders the real route table at `path` with a fixed session (no `GET /me`). */
export function renderApp({
  session,
  path = '/',
  locale = 'en',
  retry = () => undefined,
}: Options) {
  const router = createMemoryRouter(routes, { initialEntries: [path] });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const result = render(
    <QueryClientProvider client={client}>
      <I18nProvider initialLocale={locale}>
        <SessionContext.Provider value={{ session, retry }}>
          <RouterProvider router={router} />
        </SessionContext.Provider>
      </I18nProvider>
    </QueryClientProvider>,
  );
  return { ...result, router };
}
