import { fireEvent, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw/http';
import { describe, expect, it, vi } from 'vitest';
import { server } from '../../test/server';
import { renderApp, userSession } from '../../test/render';

const acceptedKey = {
  id: 5,
  label: 'My Flickr key',
  key_public: 'abcdef••••',
  per_hour_limit: 3600,
  per_second_limit: 4,
  burst: 8,
  status: 'valid',
  created_at: '2026-10-25T00:00:00Z',
};

describe('setup wizard', () => {
  it('validates key format locally before sending and checks a valid key with POST /keys', async () => {
    const create = vi.fn(() => HttpResponse.json(acceptedKey, { status: 201 }));
    server.use(
      http.get('*/keys', () => HttpResponse.json([])),
      http.post('*/keys', create),
    );
    renderApp({ session: userSession, path: '/onboarding' });

    const key = await screen.findByTestId('wizard-key-input');
    fireEvent.change(key, { target: { value: 'bad' } });
    fireEvent.change(screen.getByTestId('wizard-key-secret'), { target: { value: 'bad' } });
    fireEvent.click(screen.getByTestId('wizard-key-submit'));
    expect(await screen.findByText(/32 characters/)).toBeInTheDocument();
    expect(create).not.toHaveBeenCalled();

    fireEvent.change(key, { target: { value: '0123456789abcdef0123456789abcdef' } });
    fireEvent.change(screen.getByTestId('wizard-key-secret'), {
      target: { value: '0123456789abcdef' },
    });
    fireEvent.click(screen.getByTestId('wizard-key-submit'));
    expect(await screen.findByTestId('wizard-step-connect')).toBeInTheDocument();
    expect(create).toHaveBeenCalledOnce();
  });

  it('shows Flickr rejection and keeps the key fields for correction', async () => {
    server.use(
      http.get('*/keys', () => HttpResponse.json([])),
      http.post('*/keys', () => HttpResponse.json({ code: 'key_invalid' }, { status: 422 })),
    );
    renderApp({ session: userSession, path: '/onboarding' });
    fireEvent.change(await screen.findByTestId('wizard-key-input'), {
      target: { value: '0123456789abcdef0123456789abcdef' },
    });
    fireEvent.change(screen.getByTestId('wizard-key-secret'), {
      target: { value: '0123456789abcdef' },
    });
    fireEvent.click(screen.getByTestId('wizard-key-submit'));

    expect(await screen.findByTestId('wizard-key-error')).toHaveTextContent(
      'Flickr rejected this key',
    );
    expect(screen.getByTestId('wizard-key-input')).toHaveValue('0123456789abcdef0123456789abcdef');
    expect(screen.getByTestId('wizard-key-secret')).toHaveValue('0123456789abcdef');
  });

  it('starts OAuth for the saved key and handles an OAuth callback return', async () => {
    server.use(
      http.get('*/keys', () => HttpResponse.json([acceptedKey])),
      http.post('*/flickr-accounts/connect', () =>
        HttpResponse.json({ code: 'key_not_valid' }, { status: 409 }),
      ),
      http.get('*/flickr-accounts/callback', ({ request }) => {
        expect(new URL(request.url).searchParams.get('state')).toBe('state-1');
        return HttpResponse.json({
          id: 4,
          key_id: 5,
          nsid: 'u-1',
          username: 'Anna',
          status: 'ok',
          created_at: '2026-10-25T00:00:00Z',
        });
      }),
      http.get('*/me/crawl/queue', () =>
        HttpResponse.json({
          lanes: [
            { lane: 'person', backlog_calls: 4, effective_calls_per_hour: 3600, eta_hours: 0.01 },
          ],
        }),
      ),
    );
    const app = renderApp({ session: userSession, path: '/onboarding' });
    expect(await screen.findByTestId('wizard-step-connect')).toBeInTheDocument();
    fireEvent.click(screen.getByTestId('wizard-connect-allow'));
    expect(await screen.findByTestId('wizard-connect-error')).toHaveTextContent(
      'Could not start Flickr sign in',
    );
    app.unmount();

    const callbackApp = renderApp({
      session: userSession,
      path: '/onboarding?state=state-1&oauth_token=req-1',
    });
    await waitFor(() => expect(screen.getByTestId('wizard-step-done')).toBeInTheDocument());
    expect(await screen.findByTestId('wizard-crawl-progress')).toHaveTextContent(
      '4 calls are queued',
    );
    callbackApp.unmount();
  });

  it('shows the forbidden state when the account may not set up', async () => {
    server.use(http.get('*/keys', () => HttpResponse.json({ code: 'forbidden' }, { status: 403 })));
    renderApp({ session: userSession, path: '/onboarding' });
    expect(await screen.findByTestId('wizard-forbidden')).toBeInTheDocument();
    expect(screen.getByTestId('wizard-forbidden-cta')).toHaveAttribute('href', '/');
  });

  it('shows the load error with retry on a server failure', async () => {
    server.use(http.get('*/keys', () => HttpResponse.json({ code: 'internal' }, { status: 500 })));
    renderApp({ session: userSession, path: '/onboarding' });
    expect(await screen.findByTestId('wizard-error')).toBeInTheDocument();
    expect(screen.getByTestId('wizard-error-retry')).toBeInTheDocument();
  });

  it.each([
    [409, { code: 'key_duplicate' }, 'already in your account'],
    [503, { code: 'no_node_available' }, 'No healthy node'],
    [500, { code: 'internal' }, 'could not reach Flickr'],
  ])('maps POST /keys %s to its message and keeps the values', async (status, body, text) => {
    server.use(
      http.get('*/keys', () => HttpResponse.json([])),
      http.post('*/keys', () => HttpResponse.json(body, { status })),
    );
    renderApp({ session: userSession, path: '/onboarding' });
    fireEvent.change(await screen.findByTestId('wizard-key-input'), {
      target: { value: '0123456789abcdef0123456789abcdef' },
    });
    fireEvent.change(screen.getByTestId('wizard-key-secret'), {
      target: { value: '0123456789abcdef' },
    });
    fireEvent.click(screen.getByTestId('wizard-key-submit'));
    await waitFor(() => expect(document.body).toHaveTextContent(text));
    expect(screen.getByTestId('wizard-key-input')).toHaveValue('0123456789abcdef0123456789abcdef');
  });

  it('shows secret format error, toggles the secret and reports a network failure', async () => {
    server.use(
      http.get('*/keys', () => HttpResponse.json([])),
      http.post('*/keys', () => HttpResponse.error()),
    );
    renderApp({ session: userSession, path: '/onboarding' });
    fireEvent.change(await screen.findByTestId('wizard-key-input'), {
      target: { value: '0123456789abcdef0123456789abcdef' },
    });
    fireEvent.change(screen.getByTestId('wizard-key-secret'), { target: { value: 'short' } });
    fireEvent.click(screen.getByTestId('wizard-key-submit'));
    expect(await screen.findByTestId('wizard-key-error-secret')).toBeInTheDocument();
    fireEvent.click(screen.getByTestId('wizard-key-secret-toggle'));
    expect(screen.getByTestId('wizard-key-secret')).toHaveAttribute('type', 'text');
    fireEvent.change(screen.getByTestId('wizard-key-secret'), {
      target: { value: '0123456789abcdef' },
    });
    fireEvent.click(screen.getByTestId('wizard-key-submit'));
    expect(await screen.findByTestId('wizard-key-error')).toHaveTextContent(
      'could not reach Flickr',
    );
  });

  it.each([
    ['denied', 'did not authorize'],
    ['expired', 'expired'],
  ])('shows the %s return from Flickr on step 2', async (error, text) => {
    server.use(http.get('*/keys', () => HttpResponse.json([acceptedKey])));
    renderApp({ session: userSession, path: `/onboarding?step=2&error=${error}` });
    expect(await screen.findByTestId('wizard-connect-error')).toHaveTextContent(text);
    fireEvent.click(screen.getByTestId('wizard-connect-back'));
    expect(await screen.findByTestId('wizard-step-key')).toBeInTheDocument();
  });

  it('redirects to Flickr, offers a key select and loops back from step 3', async () => {
    const assign = vi.fn();
    vi.stubGlobal('location', { ...window.location, assign });
    server.use(
      http.get('*/keys', () =>
        HttpResponse.json([acceptedKey, { ...acceptedKey, id: 6, label: 'Second' }]),
      ),
      http.post('*/flickr-accounts/connect', () =>
        HttpResponse.json({ authorize_url: 'https://flickr.test/authorize' }),
      ),
      http.get('*/me/crawl/queue', () => HttpResponse.json({ lanes: [] })),
    );
    renderApp({ session: userSession, path: '/onboarding' });
    fireEvent.change(await screen.findByTestId('wizard-key-select'), { target: { value: '5' } });
    fireEvent.click(screen.getByTestId('wizard-connect-allow'));
    await waitFor(() => expect(assign).toHaveBeenCalledWith('https://flickr.test/authorize'));
    vi.unstubAllGlobals();
  });

  it('shows crawl progress on step 3, a callback failure, and loops to step 2', async () => {
    server.use(
      http.get('*/keys', () => HttpResponse.json([acceptedKey])),
      http.get('*/me/crawl/queue', () => HttpResponse.json({ lanes: [] })),
    );
    renderApp({ session: userSession, path: '/onboarding?step=3' });
    expect(await screen.findByTestId('wizard-crawl-progress')).toHaveTextContent('getting ready');
    fireEvent.click(screen.getByTestId('wizard-add-account'));
    expect(await screen.findByTestId('wizard-step-connect')).toBeInTheDocument();
  });

  it('reports failures of crawl progress and of the OAuth callback', async () => {
    server.use(
      http.get('*/keys', () => HttpResponse.json([acceptedKey])),
      http.get('*/me/crawl/queue', () => HttpResponse.json({ code: 'x' }, { status: 500 })),
      http.get('*/flickr-accounts/callback', () =>
        HttpResponse.json({ code: 'bad' }, { status: 400 }),
      ),
    );
    const first = renderApp({ session: userSession, path: '/onboarding?step=3' });
    expect(await screen.findByTestId('wizard-done-error')).toBeInTheDocument();
    first.unmount();
    renderApp({ session: userSession, path: '/onboarding?state=s&oauth_token=t' });
    expect(await screen.findByTestId('wizard-connect-error')).toHaveTextContent('authorize');
  });
});
