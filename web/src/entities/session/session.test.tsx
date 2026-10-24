import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, renderHook, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw/http';
import { describe, expect, it } from 'vitest';
import { server } from '../../test/server';
import { SessionProvider, fetchSession } from './SessionProvider';
import { useSession } from './model';
import type { ReactNode } from 'react';

const me = {
  id: 1,
  email: 'a@x.test',
  role: 'admin',
  locale: 'vi',
  created_at: '2026-10-01T00:00:00Z',
};

describe('fetchSession', () => {
  it('maps 200 to an authenticated session with the role', async () => {
    server.use(http.get('*/me', () => HttpResponse.json(me)));

    await expect(fetchSession()).resolves.toEqual({
      status: 'authenticated',
      user: { id: 1, email: 'a@x.test', role: 'admin', locale: 'vi' },
    });
  });

  it('maps 401 to unauthenticated and any other status to an error', async () => {
    server.use(http.get('*/me', () => new HttpResponse(null, { status: 401 })));
    await expect(fetchSession()).resolves.toEqual({ status: 'unauthenticated' });

    server.use(http.get('*/me', () => new HttpResponse(null, { status: 503 })));
    await expect(fetchSession()).rejects.toThrow(/503/);
  });
});

function Probe() {
  const { session } = useSession();
  return <p>{session.status === 'authenticated' ? session.user.role : session.status}</p>;
}

function wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

describe('SessionProvider', () => {
  it('goes from loading to the signed-in role', async () => {
    server.use(http.get('*/me', () => HttpResponse.json(me)));
    render(
      <SessionProvider>
        <Probe />
      </SessionProvider>,
      { wrapper },
    );

    expect(screen.getByText('loading')).toBeInTheDocument();
    expect(await screen.findByText('admin')).toBeInTheDocument();
  });

  it('reports an error and recovers on retry', async () => {
    server.use(http.get('*/me', () => new HttpResponse(null, { status: 500 })));
    function Retrying() {
      const { session, retry } = useSession();
      return (
        <button type="button" onClick={retry}>
          {session.status}
        </button>
      );
    }
    render(
      <SessionProvider>
        <Retrying />
      </SessionProvider>,
      { wrapper },
    );
    await screen.findByText('error');

    server.use(http.get('*/me', () => HttpResponse.json(me)));
    screen.getByRole('button').click();

    await waitFor(() => {
      expect(screen.getByRole('button')).toHaveTextContent('authenticated');
    });
  });

  it('throws when used outside the provider', () => {
    expect(() => renderHook(() => useSession())).toThrow(/inside SessionProvider/);
  });
});
