import { fireEvent, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw/http';
import { describe, expect, it } from 'vitest';
import { server } from '../../test/server';
import { renderApp, userSession } from '../../test/render';

describe('ProfilePage', () => {
  it('saves the selected language to the profile endpoint', async () => {
    let body: unknown;
    const user = userSession.status === 'authenticated' ? userSession.user : {};
    server.use(
      http.get('*/me/sessions', () =>
        HttpResponse.json({
          items: [
            {
              id: '1',
              current: true,
              user_agent_label: 'Firefox on Linux',
              ip_masked: '203.0.113.x',
              last_seen_at: '2026-10-25T10:00:00Z',
              created_at: '2026-10-20T10:00:00Z',
            },
          ],
        }),
      ),
      http.patch('*/me', async ({ request }) => {
        body = await request.json();
        return HttpResponse.json({
          ...user,
          locale: 'vi',
          created_at: '2026-10-01T00:00:00Z',
        });
      }),
    );
    renderApp({ session: userSession, path: '/profile' });
    fireEvent.change(await screen.findByTestId('profile-account-language'), {
      target: { value: 'vi' },
    });
    fireEvent.click(screen.getByTestId('profile-account-save'));

    await waitFor(() => expect(body).toEqual({ locale: 'vi' }));
    expect(await screen.findByTestId('profile-session-current')).toHaveTextContent('Thiết bị này');
  });

  it('opens the session-expired dialog when an account action returns 401', async () => {
    server.use(http.patch('*/me', () => new HttpResponse(null, { status: 401 })));
    renderApp({ session: userSession, path: '/profile' });
    fireEvent.change(await screen.findByTestId('profile-account-language'), {
      target: { value: 'vi' },
    });
    fireEvent.click(screen.getByTestId('profile-account-save'));
    expect(await screen.findByTestId('shell-session-modal')).toBeInTheDocument();
  });

  it('shows session loading errors with an independent retry control', async () => {
    let loads = 0;
    server.use(
      http.get('*/me/sessions', () => {
        loads += 1;
        return loads === 1
          ? new HttpResponse(null, { status: 500 })
          : HttpResponse.json({ items: [] });
      }),
    );
    renderApp({ session: userSession, path: '/profile' });
    expect(await screen.findByTestId('profile-error-sessions')).toHaveTextContent(
      'Could not load Active sessions',
    );
    fireEvent.click(screen.getByTestId('profile-error-retry-sessions'));
    expect(await screen.findByText('No session details are available.')).toBeInTheDocument();
  });

  it('shows a card error when loading sessions rejects at the network boundary', async () => {
    server.use(http.get('*/me/sessions', () => HttpResponse.error()));
    renderApp({ session: userSession, path: '/profile' });
    expect(await screen.findByTestId('profile-error-sessions')).toHaveTextContent(
      'Could not load Active sessions',
    );
  });

  it('validates password confirmation before sending a request', async () => {
    server.use(http.get('*/me/sessions', () => HttpResponse.json({ items: [] })));
    renderApp({ session: userSession, path: '/profile' });
    fireEvent.change(await screen.findByTestId('profile-password-current'), {
      target: { value: 'oldpassword' },
    });
    fireEvent.change(screen.getByTestId('profile-password-new'), {
      target: { value: 'newpassword' },
    });
    fireEvent.change(screen.getByTestId('profile-password-confirm'), {
      target: { value: 'different' },
    });
    fireEvent.click(screen.getByTestId('profile-password-submit'));
    expect(await screen.findByTestId('profile-password-error')).toHaveTextContent('do not match');
  });

  it.each([
    ['short', 'at least 8 characters'],
    ['x'.repeat(129), 'at most 128 characters'],
  ])('validates a new password length before sending', async (value, expected) => {
    renderApp({ session: userSession, path: '/profile' });
    fireEvent.change(await screen.findByTestId('profile-password-current'), {
      target: { value: 'old-password' },
    });
    fireEvent.change(screen.getByTestId('profile-password-new'), { target: { value } });
    fireEvent.change(screen.getByTestId('profile-password-confirm'), { target: { value } });
    fireEvent.click(screen.getByTestId('profile-password-submit'));
    expect(await screen.findByTestId('profile-password-error')).toHaveTextContent(expected);
  });

  it('updates the password, clears the fields, and refreshes sessions', async () => {
    server.use(http.post('*/me/password', () => new HttpResponse(null, { status: 204 })));
    renderApp({ session: userSession, path: '/profile' });
    fireEvent.change(await screen.findByTestId('profile-password-current'), {
      target: { value: 'old-password' },
    });
    fireEvent.change(screen.getByTestId('profile-password-new'), {
      target: { value: 'new-password' },
    });
    fireEvent.change(screen.getByTestId('profile-password-confirm'), {
      target: { value: 'new-password' },
    });
    fireEvent.click(screen.getByTestId('profile-password-submit'));
    expect(await screen.findByText('Password changed.')).toBeInTheDocument();
    expect(screen.getByTestId('profile-password-current')).toHaveValue('');
  });

  it('shows an error for the current password response', async () => {
    server.use(http.post('*/me/password', () => new HttpResponse(null, { status: 401 })));
    renderApp({ session: userSession, path: '/profile' });
    fireEvent.change(await screen.findByTestId('profile-password-current'), {
      target: { value: 'bad-password' },
    });
    fireEvent.change(screen.getByTestId('profile-password-new'), {
      target: { value: 'new-password' },
    });
    fireEvent.change(screen.getByTestId('profile-password-confirm'), {
      target: { value: 'new-password' },
    });
    fireEvent.click(screen.getByTestId('profile-password-submit'));
    expect(await screen.findByTestId('profile-password-error')).toHaveTextContent(
      'current password is not right',
    );
  });

  it('shows a general error when the password service fails', async () => {
    server.use(http.post('*/me/password', () => new HttpResponse(null, { status: 500 })));
    renderApp({ session: userSession, path: '/profile' });
    fireEvent.change(await screen.findByTestId('profile-password-current'), {
      target: { value: 'old-password' },
    });
    fireEvent.change(screen.getByTestId('profile-password-new'), {
      target: { value: 'new-password' },
    });
    fireEvent.change(screen.getByTestId('profile-password-confirm'), {
      target: { value: 'new-password' },
    });
    fireEvent.click(screen.getByTestId('profile-password-submit'));
    expect(await screen.findByTestId('profile-password-error')).toHaveTextContent(
      'action could not be completed',
    );
  });

  it('revokes one session and other sessions after confirmation', async () => {
    server.use(
      http.get('*/me/sessions', () =>
        HttpResponse.json({
          items: [
            {
              id: '1',
              current: true,
              user_agent_label: 'Current browser',
              ip_masked: '203.0.113.x',
              last_seen_at: '2026-10-25T10:00:00Z',
              created_at: '2026-10-20T10:00:00Z',
            },
            {
              id: '2',
              current: false,
              user_agent_label: 'Old browser',
              ip_masked: '203.0.113.x',
              last_seen_at: '2026-10-24T10:00:00Z',
              created_at: '2026-10-19T10:00:00Z',
            },
          ],
        }),
      ),
      http.delete('*/me/sessions/2', () => new HttpResponse(null, { status: 204 })),
    );
    renderApp({ session: userSession, path: '/profile' });
    fireEvent.click(await screen.findByTestId('profile-session-revoke-2'));
    expect(await screen.findByTestId('profile-confirm-dialog')).toHaveRole('dialog');
    fireEvent.click(screen.getByTestId('profile-confirm-ok'));
    await waitFor(() =>
      expect(screen.queryByTestId('profile-session-row-2')).not.toBeInTheDocument(),
    );
  });

  it('revokes all other sessions after confirmation', async () => {
    let revokeOthers = false;
    server.use(
      http.get('*/me/sessions', () =>
        HttpResponse.json({
          items: [
            {
              id: '1',
              current: true,
              user_agent_label: 'Current browser',
              ip_masked: '203.0.113.x',
              last_seen_at: '2026-10-25T10:00:00Z',
              created_at: '2026-10-20T10:00:00Z',
            },
            {
              id: '2',
              current: false,
              user_agent_label: 'Old browser',
              ip_masked: '203.0.113.x',
              last_seen_at: '2026-10-24T10:00:00Z',
              created_at: '2026-10-19T10:00:00Z',
            },
          ],
        }),
      ),
      http.delete('*/auth/sessions', () => {
        revokeOthers = true;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    renderApp({ session: userSession, path: '/profile' });
    await screen.findByTestId('profile-session-row-2');
    fireEvent.click(await screen.findByTestId('profile-sessions-revoke-others'));
    expect(await screen.findByTestId('profile-confirm-dialog')).toHaveRole('dialog');
    fireEvent.click(screen.getByTestId('profile-confirm-ok'));
    await waitFor(() => expect(revokeOthers).toBe(true));
  });

  it('keeps a session after revoke failure and reports the action error', async () => {
    server.use(
      http.get('*/me/sessions', () =>
        HttpResponse.json({
          items: [
            {
              id: '1',
              current: true,
              user_agent_label: 'Current',
              ip_masked: '',
              last_seen_at: '2026-10-25T10:00:00Z',
              created_at: '2026-10-20T10:00:00Z',
            },
            {
              id: '2',
              current: false,
              user_agent_label: 'Old browser',
              ip_masked: '',
              last_seen_at: '2026-10-24T10:00:00Z',
              created_at: '2026-10-19T10:00:00Z',
            },
          ],
        }),
      ),
      http.delete('*/me/sessions/2', () => new HttpResponse(null, { status: 500 })),
    );
    renderApp({ session: userSession, path: '/profile' });
    fireEvent.click(await screen.findByTestId('profile-session-revoke-2'));
    fireEvent.click(await screen.findByTestId('profile-confirm-ok'));
    expect(await screen.findByText('That action could not be completed.')).toBeInTheDocument();
    expect(screen.getByTestId('profile-session-row-2')).toBeInTheDocument();
  });

  it('traps focus in the confirmation dialog and closes on Escape', async () => {
    server.use(
      http.get('*/me/sessions', () =>
        HttpResponse.json({
          items: [
            {
              id: '1',
              current: true,
              user_agent_label: 'Current',
              ip_masked: '',
              last_seen_at: '2026-10-25T10:00:00Z',
              created_at: '2026-10-20T10:00:00Z',
            },
            {
              id: '2',
              current: false,
              user_agent_label: 'Old browser',
              ip_masked: '',
              last_seen_at: '2026-10-24T10:00:00Z',
              created_at: '2026-10-19T10:00:00Z',
            },
          ],
        }),
      ),
    );
    renderApp({ session: userSession, path: '/profile' });
    const trigger = await screen.findByTestId('profile-session-revoke-2');
    fireEvent.click(trigger);
    const dialog = await screen.findByTestId('profile-confirm-dialog');
    const cancel = screen.getByTestId('profile-confirm-cancel');
    const confirm = screen.getByTestId('profile-confirm-ok');
    confirm.focus();
    fireEvent.keyDown(dialog, { key: 'Tab' });
    expect(cancel).toHaveFocus();
    fireEvent.keyDown(dialog, { key: 'Tab', shiftKey: true });
    expect(confirm).toHaveFocus();
    fireEvent.keyDown(dialog, { key: 'Escape' });
    expect(screen.queryByTestId('profile-confirm-dialog')).not.toBeInTheDocument();
    await waitFor(() => expect(trigger).toHaveFocus());
  });

  it('shows an error when revoking other sessions fails', async () => {
    server.use(
      http.get('*/me/sessions', () =>
        HttpResponse.json({
          items: [
            {
              id: '1',
              current: true,
              user_agent_label: 'Current',
              ip_masked: '',
              last_seen_at: '2026-10-25T10:00:00Z',
              created_at: '2026-10-20T10:00:00Z',
            },
            {
              id: '2',
              current: false,
              user_agent_label: 'Old browser',
              ip_masked: '',
              last_seen_at: '2026-10-24T10:00:00Z',
              created_at: '2026-10-19T10:00:00Z',
            },
          ],
        }),
      ),
      http.delete('*/auth/sessions', () => new HttpResponse(null, { status: 500 })),
    );
    renderApp({ session: userSession, path: '/profile' });
    await screen.findByTestId('profile-session-row-2');
    fireEvent.click(await screen.findByTestId('profile-sessions-revoke-others'));
    fireEvent.click(await screen.findByTestId('profile-confirm-ok'));
    expect(await screen.findByTestId('profile-error-sessions')).toHaveTextContent(
      'Could not load Active sessions',
    );
  });

  it('signs out through the logout endpoint and clears the session cache', async () => {
    let loggedOut = false;
    server.use(
      http.get('*/me/sessions', () => HttpResponse.json({ items: [] })),
      http.post('*/auth/logout', () => {
        loggedOut = true;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const { router } = renderApp({ session: userSession, path: '/profile' });
    fireEvent.click(await screen.findByTestId('profile-account-signout'));
    await waitFor(() => expect(router.state.location.pathname).toBe('/login'));
    expect(loggedOut).toBe(true);
  });
});
