import { fireEvent, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw/http';
import { describe, expect, it, vi } from 'vitest';
import { server } from '../../test/server';
import { renderApp } from '../../test/render';
import { authenticationOptions, sessionEnvelope } from '../../test/rest';

const signedOut = { status: 'unauthenticated' as const };

describe('LoginPage', () => {
  it('submits credentials and sends a signed-in user to a safe return path', async () => {
    let posted: unknown;
    server.use(
      http.get('*/api/v1/authentication-options', () =>
        HttpResponse.json(authenticationOptions({ registration_open: false })),
      ),
      http.post('*/api/v1/sessions', async ({ request }) => {
        posted = await request.json();
        return HttpResponse.json(
          sessionEnvelope(
            { id: 'session-4', current: true },
            {
              id: 4,
              email: 'a@example.test',
              role: 'user',
              locale: 'en',
              created_at: '2026-01-01T00:00:00Z',
            },
          ),
          { status: 201 },
        );
      }),
    );
    const { router } = renderApp({ session: signedOut, path: '/login?return_to=%2Falbums%2F42' });
    fireEvent.change(await screen.findByTestId('auth-login-email'), {
      target: { value: 'a@example.test' },
    });
    fireEvent.change(screen.getByTestId('auth-login-password'), {
      target: { value: 'password123' },
    });
    fireEvent.click(screen.getByTestId('auth-login-submit'));

    await waitFor(() => expect(router.state.location.pathname).toBe('/albums/42'));
    expect(posted).toEqual({ email: 'a@example.test', password: 'password123' });
  });

  it('shows neutral invalid credentials and clears the password after an MSW error', async () => {
    server.use(http.post('*/api/v1/sessions', () => new HttpResponse(null, { status: 401 })));
    renderApp({ session: signedOut, path: '/login' });
    fireEvent.change(await screen.findByTestId('auth-login-email'), {
      target: { value: 'a@example.test' },
    });
    fireEvent.change(screen.getByTestId('auth-login-password'), {
      target: { value: 'wrong-password' },
    });
    fireEvent.click(screen.getByTestId('auth-login-submit'));

    expect(await screen.findByTestId('auth-error-banner')).toHaveTextContent('email or password');
    expect(screen.getByTestId('auth-error-email')).toBeInTheDocument();
    expect(screen.getByTestId('auth-error-password')).toBeInTheDocument();
    await waitFor(() => expect(screen.getByTestId('auth-login-password')).toHaveValue(''));
  });

  it('offers no passkey action because the backend has no passkey endpoints', async () => {
    vi.stubGlobal('PublicKeyCredential', () => undefined);
    server.use(
      http.get('*/api/v1/authentication-options', () =>
        HttpResponse.json(authenticationOptions({ passkey_enabled: true })),
      ),
    );
    renderApp({ session: signedOut, path: '/login' });
    await screen.findByTestId('auth-login-form');
    expect(screen.queryByTestId('auth-login-passkey')).not.toBeInTheDocument();
    expect(screen.queryByTestId('auth-login-divider')).not.toBeInTheDocument();
    vi.unstubAllGlobals();
  });

  it('shows the form with partial defaults when auth config fails', async () => {
    server.use(http.get('*/api/v1/authentication-options', () => HttpResponse.error()));
    renderApp({ session: signedOut, path: '/login' });
    expect(await screen.findByTestId('auth-partial-note')).toBeInTheDocument();
    expect(screen.getByTestId('auth-login-to-register')).toBeInTheDocument();
    expect(screen.queryByTestId('auth-login-passkey')).not.toBeInTheDocument();
  });

  it('rejects hostile return_to values and shows the expired-session state', async () => {
    const { router } = renderApp({
      session: signedOut,
      path: '/login?reason=expired&return_to=%2F%2Fevil.example',
    });
    expect(await screen.findByTestId('auth-expired-banner')).toBeInTheDocument();
    fireEvent.change(screen.getByTestId('auth-login-email'), {
      target: { value: 'a@example.test' },
    });
    fireEvent.change(screen.getByTestId('auth-login-password'), {
      target: { value: 'password123' },
    });
    server.use(
      http.post('*/api/v1/sessions', () =>
        HttpResponse.json(
          sessionEnvelope(
            { id: 'session-4', current: true },
            {
              id: 4,
              email: 'a@example.test',
              role: 'user',
              locale: 'en',
              created_at: '2026-01-01T00:00:00Z',
            },
          ),
          { status: 201 },
        ),
      ),
    );
    fireEvent.click(screen.getByTestId('auth-login-submit'));
    await waitFor(() => expect(router.state.location.pathname).toBe('/'));
  });

  it.each([
    [429, 'Too many attempts', 'auth-ratelimit-banner'],
    [403, 'account is disabled', 'auth-disabled-banner'],
    [500, 'could not reach XFlickr', 'auth-error-banner'],
  ])('shows the correct sign-in state for HTTP %i', async (status, message, testId) => {
    server.use(http.post('*/api/v1/sessions', () => new HttpResponse(null, { status })));
    renderApp({ session: signedOut, path: '/login' });
    fireEvent.change(await screen.findByTestId('auth-login-email'), {
      target: { value: 'a@example.test' },
    });
    fireEvent.change(screen.getByTestId('auth-login-password'), {
      target: { value: 'password123' },
    });
    fireEvent.click(screen.getByTestId('auth-login-submit'));
    const banner = await screen.findByTestId(testId);
    expect(banner.textContent?.toLowerCase()).toContain(message.toLowerCase());
  });

  it('uses Retry-After for the cooldown and re-enables sign in when it expires', async () => {
    server.use(
      http.post(
        '*/api/v1/sessions',
        () =>
          new HttpResponse(JSON.stringify({ details: { retry_after_seconds: 4 } }), {
            status: 429,
            headers: { 'Content-Type': 'application/json', 'Retry-After': '1' },
          }),
      ),
    );
    renderApp({ session: signedOut, path: '/login' });
    fireEvent.change(await screen.findByTestId('auth-login-email'), {
      target: { value: 'a@example.test' },
    });
    fireEvent.change(screen.getByTestId('auth-login-password'), {
      target: { value: 'password123' },
    });
    fireEvent.click(screen.getByTestId('auth-login-submit'));
    expect(await screen.findByTestId('auth-cooldown-countdown')).toHaveTextContent('1 seconds');
    expect(screen.getByTestId('auth-login-submit')).toBeDisabled();
    await waitFor(() => expect(screen.getByTestId('auth-login-submit')).toBeEnabled(), {
      timeout: 2500,
    });
    expect(screen.getByTestId('auth-cooldown-ended')).toHaveTextContent('try signing in again');
  });

  it('uses the account-locked problem and Retry-After for the cooldown', async () => {
    server.use(
      http.post('*/api/v1/sessions', () =>
        HttpResponse.json(
          {
            type: '/problems/account-locked',
            title: 'Account locked',
            status: 429,
            detail: 'Try again later',
            instance: '/api/v1/sessions',
            trace_id: 'test-trace',
            errors: null,
          },
          { status: 429, headers: { 'Retry-After': '2' } },
        ),
      ),
    );
    renderApp({ session: signedOut, path: '/login' });
    fireEvent.change(await screen.findByTestId('auth-login-email'), {
      target: { value: 'a@example.test' },
    });
    fireEvent.change(screen.getByTestId('auth-login-password'), {
      target: { value: 'password123' },
    });
    fireEvent.click(screen.getByTestId('auth-login-submit'));
    expect(await screen.findByTestId('auth-locked-banner')).toHaveTextContent('account is locked');
    expect(screen.getByTestId('auth-login-submit')).toBeDisabled();
    await waitFor(() => expect(screen.getByTestId('auth-login-submit')).toBeEnabled(), {
      timeout: 3000,
    });
  });

  it('validates email and password, allows password reveal and opens registration', async () => {
    const { router } = renderApp({ session: signedOut, path: '/login' });
    const submit = await screen.findByTestId('auth-login-submit');
    fireEvent.click(submit);
    expect(await screen.findByTestId('auth-error-email')).toHaveTextContent('Enter your email');
    expect(screen.getByTestId('auth-login-email')).toHaveAttribute('aria-invalid', 'true');
    fireEvent.change(screen.getByTestId('auth-login-email'), {
      target: { value: 'a@example.test' },
    });
    fireEvent.click(submit);
    expect(await screen.findByTestId('auth-error-password')).toHaveTextContent(
      'Enter your password',
    );
    const toggle = screen.getByTestId('auth-login-password-toggle');
    fireEvent.click(toggle);
    expect(screen.getByTestId('auth-login-password')).toHaveAttribute('type', 'text');
    fireEvent.click(screen.getByTestId('auth-login-to-register'));
    expect(router.state.location.pathname).toBe('/register');
  });

  it('redirects an already signed-in visitor without showing the form', async () => {
    const { router } = renderApp({
      session: {
        status: 'authenticated',
        user: {
          id: 1,
          email: 'a@example.test',
          role: 'user',
          locale: 'en',
        },
      },
      path: '/login?return_to=%2Fcontacts',
    });
    await waitFor(() => expect(router.state.location.pathname).toBe('/contacts'));
  });
});
