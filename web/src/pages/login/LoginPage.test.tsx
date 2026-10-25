import { fireEvent, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw/http';
import { describe, expect, it } from 'vitest';
import { server } from '../../test/server';
import { renderApp } from '../../test/render';

const signedOut = { status: 'unauthenticated' as const };

describe('LoginPage', () => {
  it('submits credentials and sends a signed-in user to a safe return path', async () => {
    let posted: unknown;
    server.use(
      http.get('*/auth/config', () =>
        HttpResponse.json({ registration_open: false, passkey_enabled: false }),
      ),
      http.post('*/auth/login', async ({ request }) => {
        posted = await request.json();
        return HttpResponse.json({ id: 4, email: 'a@example.test', role: 'user', locale: 'en' });
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
    server.use(http.post('*/auth/login', () => new HttpResponse(null, { status: 401 })));
    renderApp({ session: signedOut, path: '/login' });
    fireEvent.change(await screen.findByTestId('auth-login-email'), {
      target: { value: 'a@example.test' },
    });
    fireEvent.change(screen.getByTestId('auth-login-password'), {
      target: { value: 'wrong-password' },
    });
    fireEvent.click(screen.getByTestId('auth-login-submit'));

    expect(await screen.findByTestId('auth-error-banner')).toHaveTextContent('email or password');
    await waitFor(() => expect(screen.getByTestId('auth-login-password')).toHaveValue(''));
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
      http.post('*/auth/login', () =>
        HttpResponse.json({ id: 4, email: 'a@example.test', role: 'user', locale: 'en' }),
      ),
    );
    fireEvent.click(screen.getByTestId('auth-login-submit'));
    await waitFor(() => expect(router.state.location.pathname).toBe('/'));
  });

  it.each([
    [429, 'Too many attempts'],
    [423, 'account is locked'],
    [403, 'account is disabled'],
    [500, 'could not reach XFlickr'],
  ])('shows the correct sign-in state for HTTP %i', async (status, message) => {
    server.use(http.post('*/auth/login', () => new HttpResponse(null, { status })));
    renderApp({ session: signedOut, path: '/login' });
    fireEvent.change(await screen.findByTestId('auth-login-email'), {
      target: { value: 'a@example.test' },
    });
    fireEvent.change(screen.getByTestId('auth-login-password'), {
      target: { value: 'password123' },
    });
    fireEvent.click(screen.getByTestId('auth-login-submit'));
    const banner = await screen.findByRole('alert');
    expect(banner.textContent?.toLowerCase()).toContain(message.toLowerCase());
  });

  it('uses Retry-After for the cooldown and re-enables sign in when it expires', async () => {
    server.use(
      http.post(
        '*/auth/login',
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

  it('uses locked_until to show the unlock time and keep the form blocked until then', async () => {
    const lockedUntil = new Date(Date.now() + 1500).toISOString();
    server.use(
      http.post('*/auth/login', () =>
        HttpResponse.json({ details: { locked_until: lockedUntil } }, { status: 423 }),
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
    expect(await screen.findByTestId('auth-ratelimit-banner')).toHaveTextContent(
      new Date(lockedUntil).toLocaleTimeString('en'),
    );
    expect(screen.getByTestId('auth-login-submit')).toBeDisabled();
    await waitFor(() => expect(screen.getByTestId('auth-login-submit')).toBeEnabled(), {
      timeout: 3000,
    });
  });

  it('validates email and password, allows password reveal and opens registration', async () => {
    const { router } = renderApp({ session: signedOut, path: '/login' });
    const submit = await screen.findByTestId('auth-login-submit');
    fireEvent.click(submit);
    expect(await screen.findByTestId('auth-error-banner')).toHaveTextContent('valid email');
    fireEvent.change(screen.getByTestId('auth-login-email'), {
      target: { value: 'a@example.test' },
    });
    fireEvent.click(submit);
    expect(await screen.findByTestId('auth-error-banner')).toHaveTextContent('Enter your password');
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
        user: { id: 1, email: 'a@example.test', role: 'user', locale: 'en' },
      },
      path: '/login?return_to=%2Fcontacts',
    });
    await waitFor(() => expect(router.state.location.pathname).toBe('/contacts'));
  });
});
