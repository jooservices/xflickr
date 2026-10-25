import { fireEvent, render, screen } from '@testing-library/react';
import { useState } from 'react';
import { createMemoryRouter, MemoryRouter, RouterProvider } from 'react-router';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { I18nProvider } from '../shared';
import { AppErrorBoundary, RouteErrorPage } from './ErrorBoundary';

afterEach(() => vi.restoreAllMocks());

function BrokenPage(): never {
  throw new Error('private stack detail');
}

describe('global error handling', () => {
  it('shows a safe fallback outside any Router, as the app level renders it', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => undefined);
    render(
      <I18nProvider>
        <AppErrorBoundary>
          <BrokenPage />
        </AppErrorBoundary>
      </I18nProvider>,
    );

    expect(await screen.findByTestId('global-error')).toBeInTheDocument();
    expect(screen.getByText(/Support reference:/)).toBeInTheDocument();
    expect(screen.queryByText('private stack detail')).toBeNull();
    expect(screen.getByRole('button', { name: 'Go to home' })).toBeInTheDocument();
  });

  it('navigates home from a route error through the router', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => undefined);
    const router = createMemoryRouter(
      [
        { path: '/', element: <p>Home page</p> },
        {
          path: '/broken',
          loader: () => {
            throw new Error('private route stack');
          },
          element: <p>Page</p>,
          errorElement: <RouteErrorPage />,
        },
      ],
      { initialEntries: ['/broken'] },
    );
    render(
      <I18nProvider>
        <RouterProvider router={router} />
      </I18nProvider>,
    );
    fireEvent.click(await screen.findByRole('button', { name: 'Go to home' }));
    expect(await screen.findByText('Home page')).toBeInTheDocument();
  });

  it('replaces router errors without exposing their message', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => undefined);
    const router = createMemoryRouter([
      {
        path: '/',
        loader: () => {
          throw new Error('private route stack');
        },
        element: <p>Page</p>,
        errorElement: <RouteErrorPage />,
      },
    ]);
    render(
      <I18nProvider>
        <RouterProvider router={router} />
      </I18nProvider>,
    );

    expect(await screen.findByTestId('global-error')).toBeInTheDocument();
    expect(screen.queryByText('private route stack')).toBeNull();
  });

  it('recovers the app boundary when Retry is selected', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => undefined);
    function MaybeBroken({ broken }: { broken: boolean }) {
      if (broken) throw new Error('private stack');
      return <p>Recovered</p>;
    }
    function Harness() {
      const [broken, setBroken] = useState(true);
      return (
        <>
          <AppErrorBoundary>
            <MaybeBroken broken={broken} />
          </AppErrorBoundary>
          <button onClick={() => setBroken(false)}>fix</button>
        </>
      );
    }
    render(
      <I18nProvider>
        <MemoryRouter>
          <Harness />
        </MemoryRouter>
      </I18nProvider>,
    );
    expect(await screen.findByTestId('global-error')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'fix' }));
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
    expect(await screen.findByText('Recovered')).toBeInTheDocument();
  });

  it('shows the logged correlation id and keeps it across re-renders', async () => {
    const log = vi.spyOn(console, 'error').mockImplementation(() => undefined);
    const { rerender } = render(
      <I18nProvider>
        <MemoryRouter>
          <AppErrorBoundary>
            <BrokenPage />
          </AppErrorBoundary>
        </MemoryRouter>
      </I18nProvider>,
    );
    const shown = (await screen.findByText(/Support reference:/)).textContent ?? '';
    const reports = log.mock.calls.filter(([message]) => message === 'XFlickr application error');
    expect(reports).toHaveLength(1);
    const logged = (reports[0][1] as { correlationId: string }).correlationId;
    expect(shown).toContain(logged);

    rerender(
      <I18nProvider>
        <MemoryRouter>
          <AppErrorBoundary>
            <BrokenPage />
          </AppErrorBoundary>
        </MemoryRouter>
      </I18nProvider>,
    );
    expect(screen.getByText(/Support reference:/).textContent).toBe(shown);
    expect(
      log.mock.calls.filter(([message]) => message === 'XFlickr application error'),
    ).toHaveLength(1);
  });

  it('reports a route error once with the id it displays', async () => {
    const log = vi.spyOn(console, 'error').mockImplementation(() => undefined);
    const router = createMemoryRouter([
      {
        path: '/',
        loader: () => {
          throw new Error('private route stack');
        },
        element: <p>Page</p>,
        errorElement: <RouteErrorPage />,
      },
    ]);
    render(
      <I18nProvider>
        <RouterProvider router={router} />
      </I18nProvider>,
    );
    const shown = (await screen.findByText(/Support reference:/)).textContent ?? '';
    const reports = log.mock.calls.filter(([message]) => message === 'XFlickr application error');
    expect(reports).toHaveLength(1);
    expect(shown).toContain((reports[0][1] as { correlationId: string }).correlationId);
  });
});
