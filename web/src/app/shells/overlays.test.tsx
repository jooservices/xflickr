import { fireEvent, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw/http';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { renderApp, userSession } from '../../test/render';
import { server } from '../../test/server';

beforeEach(() => {
  window.localStorage.clear();
});

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
  it('shows the not-available block until downloads exist', async () => {
    renderApp({ session: userSession });

    fireEvent.click(await screen.findByTestId('shell-tray-button'));

    expect(await screen.findByTestId('shell-tray-empty')).toHaveTextContent('Not available yet');
  });
});

describe('notifications drawer', () => {
  it('shows the not-available block and closes with Esc, returning focus to the bell', async () => {
    renderApp({ session: userSession });
    const bell = await screen.findByTestId('shell-notifications-button');

    fireEvent.click(bell);
    expect(await screen.findByTestId('shell-notifications-empty')).toHaveTextContent(
      'Not available yet',
    );
    expect(screen.getByTestId('shell-notifications')).toHaveAttribute('aria-modal', 'true');

    fireEvent.keyDown(document, { key: 'Escape' });
    await waitFor(() => {
      expect(screen.queryByTestId('shell-notifications')).toBeNull();
    });
    expect(bell).toHaveFocus();
  });

  it('traps Tab inside the drawer', async () => {
    renderApp({ session: userSession });
    fireEvent.click(await screen.findByTestId('shell-notifications-button'));
    const drawer = await screen.findByTestId('shell-notifications');
    const close = screen.getByRole('button', { name: 'Close notifications' });
    expect(close).toHaveFocus();

    fireEvent.keyDown(document, { key: 'Tab' });
    expect(drawer.contains(document.activeElement)).toBe(true);

    close.focus();
    fireEvent.keyDown(document, { key: 'Tab', shiftKey: true });
    expect(drawer.contains(document.activeElement)).toBe(true);
  });
});
