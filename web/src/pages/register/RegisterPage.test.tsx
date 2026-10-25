import { fireEvent, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw/http';
import { describe, expect, it } from 'vitest';
import { server } from '../../test/server';
import { renderApp } from '../../test/render';

describe('RegisterPage', () => {
  it('validates the password and required terms before submitting', async () => {
    server.use(
      http.get('*/auth/config', () =>
        HttpResponse.json({
          registration_open: true,
          passkey_enabled: false,
          password_min: 8,
          password_max: 128,
        }),
      ),
    );
    renderApp({ session: { status: 'unauthenticated' }, path: '/register' });
    fireEvent.change(await screen.findByTestId('auth-register-email'), {
      target: { value: 'new@example.test' },
    });
    fireEvent.change(screen.getByTestId('auth-register-password'), { target: { value: 'short' } });
    fireEvent.click(screen.getByTestId('auth-register-submit'));
    expect(await screen.findByTestId('auth-error-password')).toHaveTextContent('at least 8');
  });

  it('shows the closed-registration state from public configuration', async () => {
    server.use(
      http.get('*/auth/config', () =>
        HttpResponse.json({
          registration_open: false,
          passkey_enabled: false,
          password_min: 8,
          password_max: 128,
        }),
      ),
    );
    renderApp({ session: { status: 'unauthenticated' }, path: '/register' });
    expect(await screen.findByTestId('auth-register-closed')).toHaveTextContent(
      'Registration is closed',
    );
  });

  it('maps server validation details to field errors and focuses the first invalid field', async () => {
    server.use(
      http.post('*/auth/register', () =>
        HttpResponse.json(
          {
            code: 'validation_failed',
            details: {
              fields: { email: 'invalid', password: 'too_short', accept_terms: 'required' },
            },
          },
          { status: 400 },
        ),
      ),
    );
    renderApp({ session: { status: 'unauthenticated' }, path: '/register' });
    fireEvent.change(await screen.findByTestId('auth-register-email'), {
      target: { value: 'new@example.test' },
    });
    fireEvent.change(screen.getByTestId('auth-register-password'), {
      target: { value: 'valid-password' },
    });
    fireEvent.change(screen.getByTestId('auth-register-confirm'), {
      target: { value: 'valid-password' },
    });
    fireEvent.click(screen.getByTestId('auth-register-terms'));
    fireEvent.click(screen.getByTestId('auth-register-submit'));
    expect(await screen.findByTestId('auth-error-email')).toHaveTextContent('valid email');
    expect(screen.getByTestId('auth-error-password')).toHaveTextContent('at least 8');
    expect(screen.getByTestId('auth-error-terms')).toHaveTextContent('Accept the terms');
    expect(screen.getByTestId('auth-register-email')).toHaveAttribute(
      'aria-describedby',
      'auth-error-email',
    );
    expect(screen.getByTestId('auth-register-email')).toHaveFocus();
  });

  it('changes to the closed state after registration_closed from the server', async () => {
    server.use(
      http.post('*/auth/register', () =>
        HttpResponse.json({ code: 'registration_closed' }, { status: 409 }),
      ),
    );
    renderApp({ session: { status: 'unauthenticated' }, path: '/register' });
    fireEvent.change(await screen.findByTestId('auth-register-email'), {
      target: { value: 'new@example.test' },
    });
    fireEvent.change(screen.getByTestId('auth-register-password'), {
      target: { value: 'valid-password' },
    });
    fireEvent.change(screen.getByTestId('auth-register-confirm'), {
      target: { value: 'valid-password' },
    });
    fireEvent.click(screen.getByTestId('auth-register-terms'));
    fireEvent.click(screen.getByTestId('auth-register-submit'));
    expect(await screen.findByTestId('auth-register-closed')).toHaveTextContent(
      'Registration is closed',
    );
  });

  it('validates confirmation and terms before registration', async () => {
    renderApp({ session: { status: 'unauthenticated' }, path: '/register' });
    fireEvent.change(await screen.findByTestId('auth-register-email'), {
      target: { value: 'new@example.test' },
    });
    fireEvent.change(screen.getByTestId('auth-register-password'), {
      target: { value: 'valid-password' },
    });
    fireEvent.change(screen.getByTestId('auth-register-confirm'), {
      target: { value: 'other-password' },
    });
    fireEvent.click(screen.getByTestId('auth-register-submit'));
    expect(await screen.findByTestId('auth-error-confirm')).toHaveTextContent('do not match');
    fireEvent.change(screen.getByTestId('auth-register-confirm'), {
      target: { value: 'valid-password' },
    });
    fireEvent.click(screen.getByTestId('auth-register-submit'));
    expect(await screen.findByTestId('auth-error-terms')).toHaveTextContent('Accept the terms');
  });

  it('registers and signs in before sending the new user to onboarding', async () => {
    const { router } = renderApp({ session: { status: 'unauthenticated' }, path: '/register' });
    server.use(
      http.post('*/auth/register', () =>
        HttpResponse.json(
          {
            id: 9,
            email: 'new@example.test',
            role: 'user',
            locale: 'en',
            created_at: '2026-01-01T00:00:00Z',
          },
          { status: 201 },
        ),
      ),
      http.post('*/auth/login', () =>
        HttpResponse.json({
          id: 9,
          email: 'new@example.test',
          role: 'user',
          locale: 'en',
          created_at: '2026-01-01T00:00:00Z',
        }),
      ),
    );
    fireEvent.change(await screen.findByTestId('auth-register-email'), {
      target: { value: 'new@example.test' },
    });
    fireEvent.change(screen.getByTestId('auth-register-password'), {
      target: { value: 'valid-password' },
    });
    fireEvent.change(screen.getByTestId('auth-register-confirm'), {
      target: { value: 'valid-password' },
    });
    fireEvent.click(screen.getByTestId('auth-register-terms'));
    fireEvent.click(screen.getByTestId('auth-register-submit'));
    await waitFor(() => expect(router.state.location.pathname).toBe('/onboarding'));
  });

  it('shows the email conflict returned by the server', async () => {
    server.use(http.post('*/auth/register', () => new HttpResponse(null, { status: 409 })));
    const { router } = renderApp({ session: { status: 'unauthenticated' }, path: '/register' });
    fireEvent.change(await screen.findByTestId('auth-register-email'), {
      target: { value: 'new@example.test' },
    });
    fireEvent.change(screen.getByTestId('auth-register-password'), {
      target: { value: 'valid-password' },
    });
    fireEvent.change(screen.getByTestId('auth-register-confirm'), {
      target: { value: 'valid-password' },
    });
    fireEvent.click(screen.getByTestId('auth-register-terms'));
    fireEvent.click(screen.getByTestId('auth-register-submit'));
    expect(await screen.findByTestId('auth-error-email')).toHaveTextContent('already exists');
    expect(screen.getByTestId('auth-error-email-taken-link')).toHaveTextContent('Sign in instead');
    fireEvent.click(screen.getByTestId('auth-error-email-taken-link'));
    expect(router.state.location.pathname).toBe('/login');
    expect(await screen.findByTestId('auth-login-email')).toHaveValue('new@example.test');
  });

  it.each([
    ['', 'Enter your email'],
    ['bad-email', 'Enter a valid email'],
  ])('rejects an invalid email before submitting', async (email, expected) => {
    renderApp({ session: { status: 'unauthenticated' }, path: '/register' });
    fireEvent.change(await screen.findByTestId('auth-register-email'), {
      target: { value: email },
    });
    fireEvent.click(screen.getByTestId('auth-register-submit'));
    expect(await screen.findByTestId('auth-error-email')).toHaveTextContent(expected);
  });

  it('rejects an overlong password and a password equal to the email', async () => {
    renderApp({ session: { status: 'unauthenticated' }, path: '/register' });
    fireEvent.change(await screen.findByTestId('auth-register-email'), {
      target: { value: 'same@example.test' },
    });
    fireEvent.change(screen.getByTestId('auth-register-password'), {
      target: { value: 'x'.repeat(129) },
    });
    fireEvent.click(screen.getByTestId('auth-register-submit'));
    expect(await screen.findByTestId('auth-error-password')).toHaveTextContent('at most 128');
    fireEvent.change(screen.getByTestId('auth-register-password'), {
      target: { value: 'same@example.test' },
    });
    fireEvent.click(screen.getByTestId('auth-register-submit'));
    expect(await screen.findByTestId('auth-error-password')).toHaveTextContent(
      'Different from your email',
    );
  });

  it('keeps the created account and shows sign-in when automatic login fails', async () => {
    const { router } = renderApp({ session: { status: 'unauthenticated' }, path: '/register' });
    server.use(
      http.post('*/auth/register', () =>
        HttpResponse.json(
          {
            id: 10,
            email: 'new@example.test',
            role: 'user',
            locale: 'en',
            created_at: '2026-01-01T00:00:00Z',
          },
          { status: 201 },
        ),
      ),
      http.post('*/auth/login', () => new HttpResponse(null, { status: 500 })),
    );
    fireEvent.change(await screen.findByTestId('auth-register-email'), {
      target: { value: 'new@example.test' },
    });
    fireEvent.change(screen.getByTestId('auth-register-password'), {
      target: { value: 'valid-password' },
    });
    fireEvent.change(screen.getByTestId('auth-register-confirm'), {
      target: { value: 'valid-password' },
    });
    fireEvent.click(screen.getByTestId('auth-register-terms'));
    fireEvent.click(screen.getByTestId('auth-register-submit'));
    await waitFor(() => expect(router.state.location.pathname).toBe('/login'));
  });
});
