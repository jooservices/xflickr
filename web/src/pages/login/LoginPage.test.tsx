import { fireEvent, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw/http';
import { describe, expect, it, vi } from 'vitest';
import { server } from '../../test/server';
import { renderApp } from '../../test/render';

const signedOut = { status: 'unauthenticated' as const };

function mockCredentialGet() {
  const previous = Object.getOwnPropertyDescriptor(navigator, 'credentials');
  const get = vi.fn();
  Object.defineProperty(navigator, 'credentials', { configurable: true, value: { get } });
  return {
    get,
    restore() {
      if (previous) Object.defineProperty(navigator, 'credentials', previous);
      else Reflect.deleteProperty(navigator, 'credentials');
    },
  };
}

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
    expect(screen.getByTestId('auth-error-email')).toBeInTheDocument();
    expect(screen.getByTestId('auth-error-password')).toBeInTheDocument();
    await waitFor(() => expect(screen.getByTestId('auth-login-password')).toHaveValue(''));
  });

  it('renders passkey action only when enabled and supported', async () => {
    vi.stubGlobal('PublicKeyCredential', () => undefined);
    server.use(
      http.get('*/auth/config', () =>
        HttpResponse.json({ registration_open: true, passkey_enabled: true }),
      ),
    );
    const enabled = renderApp({ session: signedOut, path: '/login' });
    expect(await screen.findByTestId('auth-login-passkey')).toBeInTheDocument();
    expect(screen.getByTestId('auth-login-divider')).toHaveTextContent('or');
    enabled.unmount();

    server.use(
      http.get('*/auth/config', () =>
        HttpResponse.json({ registration_open: true, passkey_enabled: false }),
      ),
    );
    renderApp({ session: signedOut, path: '/login' });
    await screen.findByTestId('auth-login-form');
    expect(screen.queryByTestId('auth-login-passkey')).not.toBeInTheDocument();
    expect(screen.queryByTestId('auth-login-divider')).not.toBeInTheDocument();
    vi.unstubAllGlobals();
  });

  it('completes passkey sign-in and applies the signed-in user locale', async () => {
    vi.stubGlobal('PublicKeyCredential', () => undefined);
    const bytes = new Uint8Array([1, 2, 3]).buffer;
    const credential = {
      id: 'passkey-id',
      rawId: bytes,
      type: 'public-key',
      response: {
        authenticatorData: bytes,
        clientDataJSON: bytes,
        signature: bytes,
        userHandle: null,
      },
    } as unknown as PublicKeyCredential;
    const credentialApi = mockCredentialGet();
    credentialApi.get.mockResolvedValue(credential);
    const { router } = renderApp({ session: signedOut, path: '/login?return_to=%2Fphotos' });
    server.use(
      http.get('*/auth/config', () =>
        HttpResponse.json({ registration_open: true, passkey_enabled: true }),
      ),
      http.post('*/auth/passkey/options', () =>
        HttpResponse.json({ challenge: 'AQID', allowCredentials: [] }),
      ),
      http.post('*/auth/passkey/verify', () =>
        HttpResponse.json({ id: 7, email: 'passkey@example.test', role: 'user', locale: 'vi' }),
      ),
    );
    fireEvent.click(await screen.findByTestId('auth-login-passkey'));
    await waitFor(() => expect(router.state.location.pathname).toBe('/photos'));
    expect(credentialApi.get).toHaveBeenCalled();
    expect(document.documentElement.lang).toBe('vi');
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
    credentialApi.restore();
  });

  it('shows passkey failures and treats a cancelled browser prompt silently', async () => {
    vi.stubGlobal('PublicKeyCredential', () => undefined);
    server.use(
      http.get('*/auth/config', () =>
        HttpResponse.json({ registration_open: true, passkey_enabled: true }),
      ),
      http.post('*/auth/passkey/options', () =>
        HttpResponse.json({ challenge: 'AQID', allowCredentials: [] }),
      ),
      http.post('*/auth/passkey/verify', () => new HttpResponse(null, { status: 401 })),
    );
    const credentialApi = mockCredentialGet();
    credentialApi.get.mockResolvedValue({
      id: 'credential',
      rawId: new ArrayBuffer(0),
      type: 'public-key',
      response: {
        authenticatorData: new ArrayBuffer(0),
        clientDataJSON: new ArrayBuffer(0),
        signature: new ArrayBuffer(0),
        userHandle: null,
      },
    } as unknown as PublicKeyCredential);
    renderApp({ session: signedOut, path: '/login' });
    fireEvent.click(await screen.findByTestId('auth-login-passkey'));
    expect(await screen.findByTestId('auth-error-banner')).toHaveTextContent(
      'Passkey sign-in did not work',
    );

    credentialApi.get.mockRejectedValueOnce(new DOMException('Cancelled', 'NotAllowedError'));
    fireEvent.click(screen.getByTestId('auth-login-passkey'));
    await waitFor(() => expect(screen.queryByTestId('auth-error-banner')).not.toBeInTheDocument());
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
    credentialApi.restore();
  });

  it('shows the form with partial defaults when auth config fails', async () => {
    server.use(http.get('*/auth/config', () => HttpResponse.error()));
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
      http.post('*/auth/login', () =>
        HttpResponse.json({ id: 4, email: 'a@example.test', role: 'user', locale: 'en' }),
      ),
    );
    fireEvent.click(screen.getByTestId('auth-login-submit'));
    await waitFor(() => expect(router.state.location.pathname).toBe('/'));
  });

  it.each([
    [429, 'Too many attempts', 'auth-ratelimit-banner'],
    [423, 'account is locked', 'auth-locked-banner'],
    [403, 'account is disabled', 'auth-disabled-banner'],
    [500, 'could not reach XFlickr', 'auth-error-banner'],
  ])('shows the correct sign-in state for HTTP %i', async (status, message, testId) => {
    server.use(http.post('*/auth/login', () => new HttpResponse(null, { status })));
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
    expect(await screen.findByTestId('auth-locked-banner')).toHaveTextContent(
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
        user: { id: 1, email: 'a@example.test', role: 'user', locale: 'en' },
      },
      path: '/login?return_to=%2Fcontacts',
    });
    await waitFor(() => expect(router.state.location.pathname).toBe('/contacts'));
  });
});
