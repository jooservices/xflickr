import { fireEvent, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw/http';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { renderApp, userSession } from '../../test/render';
import { server } from '../../test/server';
import { resetBootHandled } from '../guards';

beforeEach(resetBootHandled);

const trayFixture = {
  items: [
    {
      photo_id: 'p1',
      title: 'Oak',
      owner_name: 'Anna',
      thumb_url: '/t/p1.jpg',
      added_at: '2026-10-25T10:00:00Z',
    },
  ],
  count: 1,
  estimated_bytes: 2_621_440,
};

describe('avatar menu', () => {
  it('lists the items in order, closes with Esc and returns focus', async () => {
    renderApp({ session: userSession });
    const button = await screen.findByTestId('shell-avatar-button');

    fireEvent.click(button);
    expect(button).toHaveAttribute('aria-expanded', 'true');
    expect(screen.getByTestId('shell-avatar-header')).toHaveTextContent(
      'Signed in as anna (anna@example.test)',
    );
    expect(screen.getAllByRole('menuitem').map((item) => item.textContent?.trim())).toEqual([
      'Profile',
      'Flickr accounts',
      'Sessions',
      'Keyboard shortcuts',
      'Sign out',
    ]);

    fireEvent.keyDown(document, { key: 'Escape' });
    await waitFor(() => {
      expect(screen.queryByTestId('shell-avatar-menu')).toBeNull();
    });
    expect(button).toHaveFocus();
  });

  it('navigates, opens the shortcuts dialog, and closes on an outside press', async () => {
    const { router } = renderApp({ session: userSession });
    fireEvent.click(await screen.findByTestId('shell-avatar-button'));
    fireEvent.click(screen.getByTestId('shell-avatar-sessions'));
    await waitFor(() => {
      expect(router.state.location.pathname).toBe('/sessions');
    });

    fireEvent.click(screen.getByTestId('shell-avatar-button'));
    fireEvent.click(screen.getByTestId('shell-avatar-shortcuts'));
    expect(await screen.findByTestId('shell-shortcuts')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Close' }));

    fireEvent.click(screen.getByTestId('shell-avatar-button'));
    fireEvent.pointerDown(document.body);
    await waitFor(() => {
      expect(screen.queryByTestId('shell-avatar-menu')).toBeNull();
    });
  });

  it('signs out through POST /auth/logout', async () => {
    const assign = vi.fn();
    vi.stubGlobal('location', { ...window.location, assign });
    const logout = vi.fn();
    server.use(
      http.post('*/auth/logout', () => {
        logout();
        return new HttpResponse(null, { status: 204 });
      }),
    );
    renderApp({ session: userSession });

    fireEvent.click(await screen.findByTestId('shell-avatar-button'));
    fireEvent.click(screen.getByTestId('shell-avatar-sign-out'));

    await waitFor(() => {
      expect(assign).toHaveBeenCalledWith('/login');
    });
    expect(logout).toHaveBeenCalledOnce();
    vi.unstubAllGlobals();
  });
});

describe('download tray', () => {
  it('shows the empty block', async () => {
    renderApp({ session: userSession });

    fireEvent.click(await screen.findByTestId('shell-tray-button'));

    expect(await screen.findByTestId('shell-tray-empty')).toHaveTextContent('Your tray is empty');
  });

  it('shows a compact error with Retry that refetches', async () => {
    let calls = 0;
    server.use(
      http.get('*/tray', () => {
        calls += 1;
        return calls === 1
          ? new HttpResponse(null, { status: 500 })
          : HttpResponse.json(trayFixture);
      }),
    );
    renderApp({ session: userSession });

    fireEvent.click(await screen.findByTestId('shell-tray-button'));
    expect(await screen.findByTestId('shell-tray-error')).toHaveTextContent(
      'Could not load the tray.',
    );

    fireEvent.click(screen.getByTestId('shell-tray-error-retry'));
    expect(await screen.findByTestId('shell-tray-item-p1')).toHaveTextContent('Oak');
  });

  it('lists items with a summary, removes one with undo, clears and downloads', async () => {
    const removed = vi.fn();
    const restored = vi.fn();
    const cleared = vi.fn();
    server.use(
      http.get('*/tray', () => HttpResponse.json(trayFixture)),
      http.delete('*/tray/items/p1', () => {
        removed();
        return new HttpResponse(null, { status: 204 });
      }),
      http.put('*/tray/items/p1', () => {
        restored();
        return new HttpResponse(null, { status: 204 });
      }),
      http.delete('*/tray', () => {
        cleared();
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const { router } = renderApp({ session: userSession });

    const button = await screen.findByTestId('shell-tray-button');
    await waitFor(() => {
      expect(button).toHaveAccessibleName('Download tray, 12 photos');
    });
    fireEvent.click(button);
    expect(await screen.findByTestId('shell-tray-summary')).toHaveTextContent(
      '1 photo · about 2.5 MB',
    );

    fireEvent.click(screen.getByTestId('shell-tray-item-p1-remove'));
    await waitFor(() => {
      expect(removed).toHaveBeenCalledOnce();
    });
    fireEvent.click(await screen.findByTestId('shell-tray-undo'));
    await waitFor(() => {
      expect(restored).toHaveBeenCalledOnce();
    });

    fireEvent.click(screen.getByTestId('shell-tray-clear'));
    await waitFor(() => {
      expect(cleared).toHaveBeenCalledOnce();
    });

    fireEvent.click(screen.getByTestId('shell-tray-download'));
    await waitFor(() => {
      expect(router.state.location.pathname).toBe('/downloads');
    });
  });
});

describe('notifications drawer', () => {
  it('shows the empty block and closes with Esc, returning focus to the bell', async () => {
    renderApp({ session: userSession });
    const bell = await screen.findByTestId('shell-notifications-button');

    fireEvent.click(bell);
    expect(await screen.findByTestId('shell-notifications-empty')).toHaveTextContent(
      'No notifications',
    );
    expect(screen.getByTestId('shell-notifications')).toHaveAttribute('aria-modal', 'true');

    fireEvent.keyDown(document, { key: 'Escape' });
    await waitFor(() => {
      expect(screen.queryByTestId('shell-notifications')).toBeNull();
    });
    expect(bell).toHaveFocus();
  });

  it('shows an inline error with Retry', async () => {
    server.use(http.get('*/notifications', () => new HttpResponse(null, { status: 500 })));
    renderApp({ session: userSession });

    fireEvent.click(await screen.findByTestId('shell-notifications-button'));

    expect(await screen.findByTestId('shell-notifications-error')).toHaveTextContent(
      'Could not load notifications.',
    );
  });

  it('lists notifications in the active language and marks all read', async () => {
    const read = vi.fn();
    server.use(
      http.get('*/notifications', () =>
        HttpResponse.json({
          items: [
            {
              id: 'n1',
              kind: 'download',
              tone: 'ok',
              text_key: 'shell.tray.removed-toast',
              params: {},
              created_at: '2026-10-25T10:00:00Z',
              read: false,
            },
            {
              id: 'n2',
              kind: 'misc',
              tone: 'info',
              text_key: 'unknown.key',
              params: {},
              created_at: '2026-10-25T10:00:00Z',
              read: true,
            },
          ],
        }),
      ),
      http.post('*/notifications/read', async ({ request }) => {
        read(await request.json());
        return new HttpResponse(null, { status: 204 });
      }),
    );
    renderApp({ session: userSession });

    fireEvent.click(await screen.findByTestId('shell-notifications-button'));
    expect(await screen.findByTestId('shell-notifications-item-n1')).toHaveTextContent(
      'Removed from tray',
    );
    expect(screen.getByTestId('shell-notifications-item-n2')).toHaveTextContent('unknown.key');

    fireEvent.click(screen.getByTestId('shell-notifications-mark-all'));
    await waitFor(() => {
      expect(read).toHaveBeenCalledWith({ all: true });
    });
  });

  it('traps Tab inside the drawer', async () => {
    renderApp({ session: userSession });
    fireEvent.click(await screen.findByTestId('shell-notifications-button'));
    const drawer = await screen.findByTestId('shell-notifications');
    const close = screen.getByRole('button', { name: 'Close notifications' });
    expect(close).toHaveFocus();

    const last = screen.getByTestId('shell-notifications-mark-all');
    last.focus();
    fireEvent.keyDown(document, { key: 'Tab' });
    expect(close).toHaveFocus();

    close.focus();
    fireEvent.keyDown(document, { key: 'Tab', shiftKey: true });
    expect(drawer.contains(document.activeElement)).toBe(true);
  });
});
