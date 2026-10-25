import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { useState } from 'react';
import { RouterProvider } from 'react-router';
import type { DataRouter } from 'react-router';
import { SessionProvider } from '../entities';
import { I18nProvider } from '../shared';
import { createAppRouter } from './router';
import { AppErrorBoundary } from './ErrorBoundary';

type AppProps = {
  /** Lets tests supply a memory router. */
  router?: DataRouter;
  queryClient?: QueryClient;
};

export function App({ router, queryClient }: AppProps) {
  const [client] = useState(() => queryClient ?? new QueryClient());
  const [appRouter] = useState(() => router ?? createAppRouter());

  return (
    <QueryClientProvider client={client}>
      <I18nProvider>
        <SessionProvider>
          <AppErrorBoundary>
            <RouterProvider router={appRouter} />
          </AppErrorBoundary>
        </SessionProvider>
      </I18nProvider>
    </QueryClientProvider>
  );
}
