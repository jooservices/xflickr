import { QueryClient } from '@tanstack/react-query';
import { render, screen } from '@testing-library/react';
import { http, HttpResponse } from 'msw/http';
import { createMemoryRouter } from 'react-router';
import { beforeEach, describe, expect, it } from 'vitest';
import { server } from '../test/server';
import { App } from './App';

import { routes } from './router';
import { userEnvelope } from '../test/rest';

function renderBooted(path: string) {
  const router = createMemoryRouter(routes, { initialEntries: [path] });
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<App router={router} queryClient={queryClient} />);
}

describe('App', () => {
  beforeEach(() => {
    window.localStorage.clear();
  });

  it('boots with GET /api/v1/me and renders the user shell', async () => {
    server.use(
      http.get('*/api/v1/me', () =>
        HttpResponse.json(
          userEnvelope({
            id: 5,
            email: 'anna@example.test',
            role: 'user',
            locale: 'en',
            created_at: '2026-10-01T00:00:00Z',
          }),
        ),
      ),
    );
    renderBooted('/library');

    expect(screen.getByText('Loading XFlickr…')).toBeInTheDocument();
    expect(await screen.findByRole('link', { name: 'Library' })).toHaveAttribute(
      'aria-current',
      'page',
    );
  });

  it('shows the session-expired modal when /api/v1/me answers 401', async () => {
    server.use(http.get('*/api/v1/me', () => new HttpResponse(null, { status: 401 })));
    renderBooted('/');

    expect(await screen.findByTestId('shell-session-modal')).toBeInTheDocument();
  });

  it('shows the error state when /api/v1/me fails and Retry repeats the request', async () => {
    server.use(http.get('*/api/v1/me', () => new HttpResponse(null, { status: 500 })));
    renderBooted('/');
    const retry = await screen.findByTestId('shell-error-retry');

    server.use(
      http.get('*/api/v1/me', () =>
        HttpResponse.json(
          userEnvelope({
            id: 5,
            email: 'anna@example.test',
            role: 'user',
            locale: 'en',
            created_at: '2026-10-01T00:00:00Z',
          }),
        ),
      ),
    );
    retry.click();

    expect(await screen.findByRole('link', { name: 'Home' })).toHaveAttribute(
      'aria-current',
      'page',
    );
  });

  it('sends an anonymous visitor of /admin to the login page', async () => {
    server.use(http.get('*/api/v1/me', () => new HttpResponse(null, { status: 401 })));
    const router = createMemoryRouter(routes, { initialEntries: ['/admin/users'] });
    render(<App router={router} />);

    expect(await screen.findByTestId('auth-page')).toBeInTheDocument();
    expect(router.state.location.search).toBe('?next=%2Fadmin%2Fusers');
  });
});
