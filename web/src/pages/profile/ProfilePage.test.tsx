import { fireEvent, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw/http';
import { describe, expect, it, vi } from 'vitest';
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

  it('announces a locale save failure while retaining the selected language', async () => {
    server.use(http.patch('*/me', () => new HttpResponse(null, { status: 500 })));
    renderApp({ session: userSession, path: '/profile' });
    const language = await screen.findByTestId('profile-account-language');
    fireEvent.change(language, { target: { value: 'vi' } });
    fireEvent.click(screen.getByTestId('profile-account-save'));
    await waitFor(() =>
      expect(screen.getByTestId('profile-page')).toHaveTextContent(
        /Could not save your profile|Không thể lưu hồ sơ/,
      ),
    );
    expect(language).toHaveValue('vi');
    expect(screen.queryByTestId('shell-session-modal')).not.toBeInTheDocument();
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

  it('shows a current-password field error for invalid_current_password', async () => {
    server.use(
      http.post('*/me/password', () =>
        HttpResponse.json({ code: 'invalid_current_password' }, { status: 400 }),
      ),
    );
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
    expect(screen.queryByTestId('shell-session-modal')).not.toBeInTheDocument();
  });

  it('opens the expired-session dialog when password change returns 401', async () => {
    server.use(http.post('*/me/password', () => new HttpResponse(null, { status: 401 })));
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
    expect(await screen.findByTestId('shell-session-modal')).toBeInTheDocument();
    expect(screen.queryByTestId('profile-password-error')).toBeNull();
  });

  it('blocks navigation while account edits are unsaved and allows discard', async () => {
    const { router } = renderApp({ session: userSession, path: '/profile' });
    fireEvent.change(await screen.findByTestId('profile-account-language'), {
      target: { value: 'vi' },
    });
    await router.navigate('/library');
    expect(await screen.findByTestId('profile-unsaved-dialog')).toBeInTheDocument();
    fireEvent.click(screen.getByText('Leave page'));
    await waitFor(() => expect(router.state.location.pathname).toBe('/library'));
  });

  it('renders the required timezone and password guidance hooks', async () => {
    renderApp({ session: userSession, path: '/profile' });
    expect(await screen.findByTestId('profile-account-timezone')).toBeInTheDocument();
    expect(screen.getByTestId('profile-password-rules')).toHaveTextContent('8 characters');
  });

  it('shows a changed-elsewhere notice when another tab updates the account', async () => {
    renderApp({ session: userSession, path: '/profile' });
    fireEvent.change(await screen.findByTestId('profile-account-language'), {
      target: { value: 'vi' },
    });
    window.dispatchEvent(new StorageEvent('storage', { key: 'xf.profile.updated', newValue: '1' }));
    expect(await screen.findByTestId('profile-changed-note')).toHaveTextContent(
      'This changed in another tab.',
    );
  });

  it('keeps the last sessions visible and marks them stale after a failed focus refresh', async () => {
    let calls = 0;
    server.use(
      http.get('*/me/sessions', () => {
        calls += 1;
        return calls === 1
          ? HttpResponse.json({
              items: [
                {
                  id: '1',
                  current: true,
                  user_agent_label: 'Current browser',
                  ip_masked: '203.0.113.x',
                  last_seen_at: '2026-10-25T10:00:00Z',
                  created_at: '2026-10-20T10:00:00Z',
                },
              ],
            })
          : new HttpResponse(null, { status: 500 });
      }),
    );
    renderApp({ session: userSession, path: '/profile' });
    await screen.findByTestId('profile-session-current');
    const now = Date.now();
    vi.spyOn(Date, 'now').mockReturnValue(now + 61_000);
    window.dispatchEvent(new Event('focus'));
    expect(await screen.findByTestId('profile-stale-note')).toBeInTheDocument();
    expect(screen.getByTestId('profile-session-row-1')).toBeInTheDocument();
    vi.restoreAllMocks();
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
    expect(await screen.findByTestId('profile-stale-note')).toHaveTextContent(
      'Showing the last loaded data',
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
