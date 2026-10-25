import { act, fireEvent, screen, waitFor } from '@testing-library/react';
import { delay, http, HttpResponse } from 'msw';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { server } from '../../test/server';
import { renderApp, userSession } from '../../test/render';
import { tileBackground } from './SetupPage';

const KEY = '0123456789abcdef0123456789abcdef';
const SECRET = '0123456789abcdef';

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
const account = {
  id: 9,
  key_id: 5,
  nsid: 'u-1',
  username: 'Anna',
  status: 'ok',
  created_at: '2026-10-25T00:00:00Z',
};

const keysReply = (keys: unknown[] = []) => http.get('*/keys', () => HttpResponse.json(keys));
const accountsReply = (accounts: unknown[] = []) =>
  http.get('*/flickr-accounts', () => HttpResponse.json(accounts));
const settingsReply = (
  settings: Record<string, unknown> = { depth: 2, depth_default: 2, depth_max: 4 },
) => http.get('*/me/crawl/settings', () => HttpResponse.json(settings));
const crawlStart = (status: number, body: Record<string, unknown> = {}) =>
  http.put('*/me/crawl/settings', () => HttpResponse.json(body, { status }));
const postKey = (status: number, body: unknown) =>
  http.post('*/keys', () => HttpResponse.json(body as Record<string, unknown>, { status }));

const open = (path = '/onboarding') => renderApp({ session: userSession, path });

async function fillAndSubmit(key = KEY, secret = SECRET) {
  fireEvent.change(await screen.findByTestId('wizard-key-input'), { target: { value: key } });
  fireEvent.change(screen.getByTestId('wizard-key-secret'), { target: { value: secret } });
  fireEvent.click(screen.getByTestId('wizard-key-submit'));
}

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  document.cookie = 'xf_csrf=; Max-Age=0; path=/';
});

describe('setup wizard bad responses', () => {
  it.each([
    [
      'HTML with status 200',
      () =>
        new HttpResponse('<!doctype html><html>app</html>', {
          status: 200,
          headers: { 'Content-Type': 'text/html' },
        }),
    ],
    ['a null body', () => HttpResponse.json(null)],
  ])('shows the retryable error when the keys list is %s', async (_name, reply) => {
    server.use(
      http.get('*/keys', () => reply()),
      accountsReply(),
    );
    open();
    expect(await screen.findByTestId('wizard-error')).toBeInTheDocument();
    expect(screen.getByTestId('wizard-error-retry')).toBeEnabled();
  });

  it.each([
    [
      'HTML with status 200',
      () =>
        new HttpResponse('<!doctype html><html>app</html>', {
          status: 200,
          headers: { 'Content-Type': 'text/html' },
        }),
    ],
    ['a null body', () => HttpResponse.json(null)],
  ])('does not crash when the accounts list is %s', async (_name, reply) => {
    server.use(
      keysReply([acceptedKey]),
      http.get('*/flickr-accounts', () => reply()),
    );
    open();
    expect(await screen.findByTestId('wizard-error')).toBeInTheDocument();
    expect(screen.queryByText(/Unexpected Application Error/)).toBeNull();
  });
});

describe('setup wizard step 1', () => {
  it('starts at step 1 for a first visit with the explainer and all fields', async () => {
    server.use(keysReply(), accountsReply());
    open();

    expect(await screen.findByTestId('wizard-step-key')).toBeInTheDocument();
    expect(screen.getByTestId('wizard-title')).toHaveFocus();
    expect(screen.getByTestId('wizard-eyebrow')).toHaveTextContent('Step 1 of 3 · Your API key');
    expect(screen.getByTestId('wizard-steps')).toHaveAttribute(
      'aria-valuetext',
      'Step 1 of 3 · Your API key',
    );
    expect(screen.getByTestId('wizard-key-why')).toBeInTheDocument();
    expect(screen.getByTestId('wizard-key-create-link')).toHaveAttribute('target', '_blank');
    expect(screen.getByTestId('wizard-art')).toHaveAttribute('aria-hidden', 'true');
    expect(screen.queryByTestId('wizard-key-back')).toBeNull();
  });

  it('validates on blur of a touched field, then on submit, and focuses the first invalid field', async () => {
    const create = vi.fn(() => HttpResponse.json(acceptedKey, { status: 201 }));
    server.use(keysReply(), accountsReply(), http.post('*/keys', create));
    open();

    const key = await screen.findByTestId('wizard-key-input');
    expect(screen.queryByTestId('wizard-key-error-key')).toBeNull();
    fireEvent.change(key, { target: { value: 'bad' } });
    expect(screen.queryByTestId('wizard-key-error-key')).toBeNull();
    fireEvent.blur(key);
    expect(screen.getByTestId('wizard-key-error-key')).toHaveTextContent('32 characters');
    expect(key).toHaveAttribute('aria-invalid', 'true');

    fireEvent.click(screen.getByTestId('wizard-key-submit'));
    await waitFor(() => expect(key).toHaveFocus());
    fireEvent.change(key, { target: { value: KEY } });
    expect(screen.queryByTestId('wizard-key-error-key')).toBeNull();

    fireEvent.change(screen.getByTestId('wizard-key-secret'), { target: { value: 'short' } });
    fireEvent.click(screen.getByTestId('wizard-key-submit'));
    expect(await screen.findByTestId('wizard-key-error-secret')).toBeInTheDocument();
    await waitFor(() => expect(screen.getByTestId('wizard-key-secret')).toHaveFocus());
    expect(create).not.toHaveBeenCalled();

    fireEvent.click(screen.getByTestId('wizard-key-secret-toggle'));
    expect(screen.getByTestId('wizard-key-secret')).toHaveAttribute('type', 'text');
    expect(screen.getByTestId('wizard-key-secret-toggle')).toHaveAttribute('aria-pressed', 'true');
  });

  it('trims spaces, posts the key once, announces it and moves to step 2', async () => {
    const bodies: unknown[] = [];
    document.cookie = 'xf_csrf=wizard-token; path=/';
    server.use(
      keysReply(),
      accountsReply(),
      http.post('*/keys', async ({ request }) => {
        if (request.headers.get('X-CSRF-Token') !== 'wizard-token') {
          return HttpResponse.json(
            { code: 'csrf_rejected', message: 'CSRF token is missing or invalid' },
            { status: 403 },
          );
        }
        bodies.push(await request.json());
        return HttpResponse.json(acceptedKey, { status: 201 });
      }),
    );
    open();
    await fillAndSubmit(` ${KEY}\n`, ` ${SECRET} `);

    expect(await screen.findByTestId('wizard-step-connect')).toBeInTheDocument();
    expect(bodies).toEqual([{ label: 'My Flickr key', key: KEY, secret: SECRET }]);
    expect(screen.getByTestId('wizard-live-region')).toHaveTextContent('Key accepted.');
    expect(screen.getByTestId('wizard-title')).toHaveFocus();
    expect(screen.getByTestId('wizard-eyebrow')).toHaveTextContent('Step 2 of 3');
  });

  it('shows key_invalid as a banner only and keeps the values', async () => {
    server.use(keysReply(), accountsReply(), postKey(422, { code: 'key_invalid' }));
    open();
    await fillAndSubmit();

    const banner = await screen.findByTestId('wizard-key-error');
    expect(banner).toHaveTextContent('Flickr rejected this key');
    expect(banner).toHaveFocus();
    expect(screen.queryByTestId('wizard-key-error-key')).toBeNull();
    expect(screen.getByTestId('wizard-key-input')).toHaveAttribute('aria-invalid', 'true');
    expect(screen.getByTestId('wizard-key-input')).toHaveValue(KEY);
    expect(screen.getByTestId('wizard-key-secret')).toHaveValue(SECRET);
    expect(screen.queryAllByText(/Flickr rejected this key/)).toHaveLength(1);
  });

  it('shows key_duplicate as a field error only and clears it on edit', async () => {
    server.use(keysReply(), accountsReply(), postKey(409, { code: 'key_duplicate' }));
    open();
    await fillAndSubmit();

    expect(await screen.findByTestId('wizard-key-error-key')).toHaveTextContent(
      'already in your account',
    );
    expect(screen.queryByTestId('wizard-key-error')).toBeNull();
    await waitFor(() => expect(screen.getByTestId('wizard-key-input')).toHaveFocus());
    fireEvent.change(screen.getByTestId('wizard-key-input'), { target: { value: 'f'.repeat(32) } });
    expect(screen.queryByTestId('wizard-key-error-key')).toBeNull();
  });

  it('maps 400 validation_failed details.fields to field errors, not a banner', async () => {
    server.use(
      keysReply(),
      accountsReply(),
      postKey(400, {
        code: 'validation_failed',
        message: 'x',
        details: { fields: { secret: 'bad' } },
      }),
    );
    open();
    await fillAndSubmit();

    expect(await screen.findByTestId('wizard-key-error-secret')).toBeInTheDocument();
    expect(screen.queryByTestId('wizard-key-error')).toBeNull();
    await waitFor(() => expect(screen.getByTestId('wizard-key-secret')).toHaveFocus());
  });

  it('maps validation_failed without known fields to the network banner', async () => {
    server.use(keysReply(), accountsReply(), postKey(400, { code: 'validation_failed' }));
    open();
    await fillAndSubmit();
    expect(await screen.findByTestId('wizard-key-error')).toHaveTextContent('could not reach');
  });

  it.each([
    [503, { code: 'no_node_available' }, 'No healthy node'],
    [429, { code: 'rate_limited' }, 'could not reach Flickr'],
    [500, { code: 'internal' }, 'could not reach Flickr'],
    [422, { code: 'other' }, 'could not reach Flickr'],
  ])('maps POST /keys %s to a banner and keeps the values', async (status, body, text) => {
    server.use(keysReply(), accountsReply(), postKey(status, body));
    open();
    await fillAndSubmit();

    expect(await screen.findByTestId('wizard-key-error')).toHaveTextContent(text);
    expect(screen.getByTestId('wizard-key-input')).toHaveValue(KEY);
  });

  it('reports a network failure with Try again that resubmits', async () => {
    let calls = 0;
    server.use(
      keysReply(),
      accountsReply(),
      http.post('*/keys', () => {
        calls += 1;
        return calls === 1 ? HttpResponse.error() : HttpResponse.json(acceptedKey, { status: 201 });
      }),
    );
    open();
    await fillAndSubmit();

    const banner = await screen.findByTestId('wizard-key-error');
    expect(banner).toHaveTextContent('could not reach Flickr');
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
    expect(await screen.findByTestId('wizard-step-connect')).toBeInTheDocument();
  });

  it('ignores a double submit while the request is in flight', async () => {
    const create = vi.fn(async () => {
      await delay(50);
      return HttpResponse.json(acceptedKey, { status: 201 });
    });
    server.use(keysReply(), accountsReply(), http.post('*/keys', create));
    open();
    await fillAndSubmit();
    expect(await screen.findByTestId('wizard-key-submit')).toBeDisabled();
    fireEvent.submit(screen.getByTestId('wizard-key-form'));
    await screen.findByTestId('wizard-step-connect');
    expect(create).toHaveBeenCalledTimes(1);
  });
});

describe('setup wizard step 2', () => {
  it('resumes at step 2 when a valid key exists and no account', async () => {
    server.use(keysReply([acceptedKey]), accountsReply());
    open();
    expect(await screen.findByTestId('wizard-step-connect')).toBeInTheDocument();
    expect(screen.getByTestId('wizard-connect-allow')).toHaveAttribute(
      'aria-describedby',
      'wizard-leave',
    );
    expect(document.getElementById('wizard-leave')).toHaveTextContent('You will leave XFlickr');
    expect(screen.queryByTestId('wizard-connect-cancel')).toBeNull();
    fireEvent.click(screen.getByTestId('wizard-connect-back'));
    expect(await screen.findByTestId('wizard-step-key')).toBeInTheDocument();
  });

  it('redirects to Flickr with the selected key and offers a key select', async () => {
    const assign = vi.fn();
    vi.stubGlobal('location', { ...window.location, assign });
    document.cookie = 'xf_csrf=wizard-token; path=/';
    const bodies: unknown[] = [];
    server.use(
      keysReply([acceptedKey, { ...acceptedKey, id: 6, label: 'Second' }]),
      accountsReply(),
      http.post('*/flickr-accounts/connect', async ({ request }) => {
        if (request.headers.get('X-CSRF-Token') !== 'wizard-token') {
          return HttpResponse.json(
            { code: 'csrf_rejected', message: 'CSRF token is missing or invalid' },
            { status: 403 },
          );
        }
        bodies.push(await request.json());
        return HttpResponse.json({ authorize_url: 'https://flickr.test/authorize' });
      }),
    );
    open();
    fireEvent.change(await screen.findByTestId('wizard-key-select'), { target: { value: '5' } });
    fireEvent.click(screen.getByTestId('wizard-connect-allow'));
    await waitFor(() => expect(assign).toHaveBeenCalledWith('https://flickr.test/authorize'));
    expect(bodies).toEqual([{ key_id: 5 }]);
    expect(screen.getByTestId('wizard-connect-waiting')).toHaveTextContent('Waiting for Flickr');
  });

  it('cancels a pending connect call and returns to idle', async () => {
    let aborted = false;
    server.use(
      keysReply([acceptedKey]),
      accountsReply(),
      http.post('*/flickr-accounts/connect', async ({ request }) => {
        request.signal.addEventListener('abort', () => {
          aborted = true;
        });
        await delay(5000);
        return HttpResponse.json({ authorize_url: 'https://flickr.test/authorize' });
      }),
    );
    open();
    fireEvent.click(await screen.findByTestId('wizard-connect-allow'));
    expect(await screen.findByTestId('wizard-connect-waiting')).toBeInTheDocument();
    expect(screen.getByTestId('wizard-connect-allow')).toBeDisabled();
    fireEvent.click(screen.getByTestId('wizard-connect-cancel'));

    expect(screen.queryByTestId('wizard-connect-waiting')).toBeNull();
    expect(screen.queryByTestId('wizard-connect-error')).toBeNull();
    expect(screen.getByTestId('wizard-connect-allow')).toBeEnabled();
    await waitFor(() => expect(aborted).toBe(true));
  });

  it.each([
    [409, { code: 'key_not_valid' }],
    [503, { code: 'x' }],
  ])('shows the start failure for connect %s with a working retry', async (status, body) => {
    const assign = vi.fn();
    vi.stubGlobal('location', { ...window.location, assign });
    let calls = 0;
    server.use(
      keysReply([acceptedKey]),
      accountsReply(),
      http.post('*/flickr-accounts/connect', () => {
        calls += 1;
        return calls === 1
          ? HttpResponse.json(body, { status })
          : HttpResponse.json({ authorize_url: 'https://flickr.test/a' });
      }),
    );
    open();
    fireEvent.click(await screen.findByTestId('wizard-connect-allow'));
    expect(await screen.findByTestId('wizard-connect-error')).toHaveTextContent(
      'Could not start the Flickr connection',
    );
    fireEvent.click(screen.getByTestId('wizard-connect-retry'));
    await waitFor(() => expect(assign).toHaveBeenCalledWith('https://flickr.test/a'));
  });

  it('shows a network failure of connect as the start failure', async () => {
    server.use(
      keysReply([acceptedKey]),
      accountsReply(),
      http.post('*/flickr-accounts/connect', () => HttpResponse.error()),
    );
    open();
    fireEvent.click(await screen.findByTestId('wizard-connect-allow'));
    expect(await screen.findByTestId('wizard-connect-error')).toBeInTheDocument();
  });

  it.each([
    ['denied', 'wizard-connect-denied', 'You declined on Flickr'],
    ['expired', 'wizard-connect-expired', 'sign-in expired'],
  ])('shows the %s return from Flickr with Try again', async (error, testId, text) => {
    const assign = vi.fn();
    vi.stubGlobal('location', { ...window.location, assign });
    server.use(
      keysReply([acceptedKey]),
      accountsReply(),
      http.post('*/flickr-accounts/connect', () =>
        HttpResponse.json({ authorize_url: 'https://flickr.test/again' }),
      ),
    );
    open(`/onboarding?step=2&error=${error}`);
    expect(await screen.findByTestId(testId)).toHaveTextContent(text);
    fireEvent.click(screen.getByTestId('wizard-connect-retry'));
    await waitFor(() => expect(assign).toHaveBeenCalledWith('https://flickr.test/again'));
  });
});

describe('OAuth return', () => {
  it('shows the connected account from the server redirect and removes the query parameter', async () => {
    server.use(keysReply([acceptedKey]), accountsReply([account]), settingsReply());
    const { router } = open('/onboarding?connected=1');

    expect(await screen.findByTestId('wizard-step-done')).toBeInTheDocument();
    expect(screen.getByTestId('wizard-live-region')).toHaveTextContent('Anna · Connected');
    await waitFor(() => expect(router.state.location.search).toBe(''));
  });

  it('focuses and announces the new account row, in English and Vietnamese', async () => {
    server.use(keysReply([acceptedKey]), accountsReply([account]), settingsReply());
    open('/onboarding?step=3&connected=9');

    const row = await screen.findByTestId('wizard-account-u-1');
    await waitFor(() => expect(row).toHaveFocus());
    expect(screen.getByTestId('wizard-live-region')).toHaveTextContent('Anna · Connected');
    expect(screen.getByTestId('wizard-account-status-u-1')).toHaveTextContent('Connected');
    expect(row).toHaveTextContent('Key My Flickr key · read permission');
    expect(screen.getByTestId('wizard-accounts')).toBeInTheDocument();

    fireEvent.click(screen.getByTestId('shell-lang-toggle'));
    expect(await screen.findByTestId('wizard-eyebrow')).toHaveTextContent('Bước 3/3');
  });

  it.each([
    ['/onboarding?connect_error=authorization_denied', 'wizard-connect-denied'],
    ['/onboarding?connect_error=invalid_state', 'wizard-connect-expired'],
    ['/onboarding?connect_error=flickr_unavailable', 'wizard-connect-error'],
  ])('shows redirect %s as %s on step 2', async (path, testId) => {
    server.use(keysReply([acceptedKey]), accountsReply());
    const { router } = open(path);
    expect(await screen.findByTestId(testId)).toBeInTheDocument();
    expect(screen.getByTestId('wizard-step-connect')).toBeInTheDocument();
    await waitFor(() => expect(router.state.location.search).toBe(''));
  });
});

describe('setup wizard step 3 and resume', () => {
  it('resumes at step 3 for a user with accounts, whatever the URL says', async () => {
    server.use(keysReply([acceptedKey]), accountsReply([account]), settingsReply());
    open('/onboarding?step=1');
    expect(await screen.findByTestId('wizard-step-done')).toBeInTheDocument();
  });

  it('opens step 3 from the server data with the accounts, depth and actions', async () => {
    server.use(keysReply([acceptedKey]), accountsReply([account]), settingsReply());
    open();
    expect(await screen.findByTestId('wizard-step-done')).toBeInTheDocument();
    expect(screen.getByTestId('wizard-title')).toHaveTextContent('You are connected');
    expect(screen.getByTestId('wizard-finish')).toHaveTextContent('Start crawling and go to Home');
    expect(screen.getByTestId('wizard-add-key')).toHaveTextContent('Use another API key');

    fireEvent.click(screen.getByTestId('wizard-add-account'));
    expect(await screen.findByTestId('wizard-step-connect')).toBeInTheDocument();
  });

  it('goes Back from step 3 to step 2 and from step 1 back to step 3', async () => {
    server.use(keysReply([acceptedKey]), accountsReply([account]), settingsReply());
    open();
    fireEvent.click(await screen.findByTestId('wizard-done-back'));
    fireEvent.click(await screen.findByTestId('wizard-connect-back'));
    fireEvent.click(await screen.findByTestId('wizard-key-back'));
    expect(await screen.findByTestId('wizard-step-done')).toBeInTheDocument();
  });

  it('shows the empty state when ?step=3 is forced without an account', async () => {
    server.use(keysReply([acceptedKey]), accountsReply());
    open('/onboarding?step=3');

    expect(await screen.findByTestId('wizard-empty')).toBeInTheDocument();
    expect(screen.queryByTestId('wizard-finish')).toBeNull();
    fireEvent.click(screen.getByTestId('wizard-empty-cta'));
    expect(await screen.findByTestId('wizard-step-connect')).toBeInTheDocument();
  });

  it('ignores ?step=3 and ?step=2 without any key', async () => {
    server.use(keysReply(), accountsReply());
    const first = open('/onboarding?step=3');
    expect(await screen.findByTestId('wizard-step-key')).toBeInTheDocument();
    first.unmount();
    open('/onboarding?step=2&error=denied');
    expect(await screen.findByTestId('wizard-step-key')).toBeInTheDocument();
  });

  it('hides Start when no account is ok and labels the revoked account', async () => {
    server.use(
      keysReply([acceptedKey]),
      accountsReply([{ ...account, status: 'revoked' }]),
      settingsReply(),
    );
    open();
    expect(await screen.findByTestId('wizard-account-status-u-1')).toHaveTextContent(
      'Access was revoked',
    );
    expect(screen.queryByTestId('wizard-finish')).toBeNull();
  });

  it('keeps the list with a stale banner when a refetch fails, and retries', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    let calls = 0;
    server.use(
      keysReply([acceptedKey]),
      settingsReply(),
      http.get('*/flickr-accounts', () => {
        calls += 1;
        return calls === 2
          ? HttpResponse.json({ code: 'x' }, { status: 500 })
          : HttpResponse.json([account]);
      }),
    );
    open();
    await screen.findByTestId('wizard-account-u-1');
    await act(async () => {
      await vi.advanceTimersByTimeAsync(10_500);
    });
    expect(await screen.findByTestId('wizard-stale-banner')).toHaveTextContent('out of date');
    expect(screen.getByTestId('wizard-account-u-1')).toBeInTheDocument();
    fireEvent.click(screen.getByTestId('wizard-stale-retry'));
    await waitFor(() => expect(screen.queryByTestId('wizard-stale-banner')).toBeNull());
  });

  it('polls accounts on step 3 and shows an account that appears', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    let list: unknown[] = [account];
    server.use(
      keysReply([acceptedKey]),
      settingsReply(),
      http.get('*/flickr-accounts', () => HttpResponse.json(list)),
    );
    open();
    await screen.findByTestId('wizard-account-u-1');
    list = [account, { ...account, id: 10, nsid: 'u-2', username: 'Bob' }];
    await act(async () => {
      await vi.advanceTimersByTimeAsync(10_500);
    });
    expect(await screen.findByTestId('wizard-account-u-2')).toBeInTheDocument();
  });
});

describe('setup wizard states', () => {
  it('shows the skeleton only after the delay', async () => {
    server.use(
      http.get('*/keys', async () => {
        await delay(400);
        return HttpResponse.json([]);
      }),
      accountsReply(),
    );
    open();
    expect(screen.queryByTestId('wizard-skeleton')).toBeNull();
    expect(screen.getByTestId('wizard-body')).toHaveAttribute('aria-busy', 'true');
    const skeleton = await screen.findByTestId('wizard-skeleton');
    expect(skeleton).toHaveAttribute('role', 'status');
    expect(skeleton).toHaveTextContent('Loading setup…');
    expect(await screen.findByTestId('wizard-step-key')).toBeInTheDocument();
  });

  it('shows the forbidden state for a 403 on either load', async () => {
    server.use(
      keysReply([acceptedKey]),
      http.get('*/flickr-accounts', () =>
        HttpResponse.json({ code: 'forbidden' }, { status: 403 }),
      ),
    );
    open();
    expect(await screen.findByTestId('wizard-forbidden')).toBeInTheDocument();
    expect(screen.getByTestId('wizard-forbidden-cta')).toHaveAttribute('href', '/');
    expect(screen.queryByTestId('wizard-error-retry')).toBeNull();
  });

  it('shows the load error and retries only the failed request', async () => {
    let keyCalls = 0;
    server.use(
      http.get('*/keys', () => {
        keyCalls += 1;
        return keyCalls === 1
          ? HttpResponse.json({ code: 'internal' }, { status: 500 })
          : HttpResponse.json([]);
      }),
      accountsReply(),
    );
    open();
    expect(await screen.findByTestId('wizard-error')).toBeInTheDocument();
    fireEvent.click(screen.getByTestId('wizard-error-retry'));
    expect(await screen.findByTestId('wizard-step-key')).toBeInTheDocument();
  });

  it('fails the load when the account list fails', async () => {
    server.use(
      keysReply(),
      http.get('*/flickr-accounts', () => HttpResponse.json({ code: 'x' }, { status: 500 })),
    );
    open();
    expect(await screen.findByTestId('wizard-error')).toBeInTheDocument();
  });

  it('shows a missing account list endpoint as the load error, not as an empty list', async () => {
    server.use(
      keysReply([acceptedKey]),
      http.get('*/flickr-accounts', () => new HttpResponse(null, { status: 404 })),
    );
    open();
    expect(await screen.findByTestId('wizard-error')).toBeInTheDocument();
    expect(screen.queryByTestId('wizard-step-connect')).toBeNull();
  });

  it('switches between English and Vietnamese on the wizard', async () => {
    server.use(keysReply(), accountsReply());
    open();
    expect(await screen.findByTestId('wizard-title')).toHaveTextContent(
      'Bring your own Flickr API key',
    );
    fireEvent.click(screen.getByTestId('shell-lang-toggle'));
    await waitFor(() =>
      expect(screen.getByTestId('wizard-title')).toHaveTextContent('Dùng khóa API Flickr'),
    );
    expect(screen.getByTestId('wizard-key-submit')).toHaveTextContent('Kiểm tra khóa');
  });

  it('draws mosaic tiles from the palette and falls back when it is empty', () => {
    expect(tileBackground(['#111,#222'], 3)).toBe('linear-gradient(135deg,#111,#222)');
    expect(tileBackground([], 3)).toBe('var(--sunken)');
  });
});

describe('setup wizard step 3 crawl start', () => {
  const stepThree = (...extra: Parameters<typeof server.use>) => {
    server.use(keysReply([acceptedKey]), accountsReply([account]), ...extra);
    return open();
  };

  it('defaults the depth to 2 and caps the slider at depth_max', async () => {
    stepThree(settingsReply({ depth: 2, depth_default: 2, depth_max: 4 }));
    const slider = await screen.findByTestId('wizard-depth');
    await waitFor(() => expect(slider).toHaveAttribute('max', '4'));
    expect(slider).toHaveAttribute('min', '1');
    expect(slider).toHaveValue('2');
    expect(screen.getByTestId('wizard-depth-value')).toHaveTextContent('Crawl depth: 2');
    fireEvent.change(slider, { target: { value: '4' } });
    expect(screen.getByTestId('wizard-depth-value')).toHaveTextContent('Crawl depth: 4');
  });

  it('starts at the saved depth but never above the cap', async () => {
    stepThree(settingsReply({ depth: 9, depth_default: 2, depth_max: 3 }));
    await waitFor(() => expect(screen.getByTestId('wizard-depth')).toHaveValue('3'));
  });

  it('posts the chosen depth, goes Home after 200 and shows the toast there', async () => {
    const start = vi.fn(async ({ request }: { request: Request }) => {
      expect(await request.json()).toEqual({ depth: 3 });
      return HttpResponse.json({ depth: 3, depth_default: 2, depth_max: 4 });
    });
    const { router } = stepThree(settingsReply(), http.put('*/me/crawl/settings', start));
    await waitFor(() => expect(screen.getByTestId('wizard-depth')).toHaveAttribute('max', '4'));
    fireEvent.change(screen.getByTestId('wizard-depth'), { target: { value: '3' } });
    fireEvent.click(screen.getByTestId('wizard-finish'));

    await waitFor(() => expect(router.state.location.pathname).toBe('/'));
    expect(start).toHaveBeenCalledTimes(1);
    expect(await screen.findByText('Crawling has started')).toBeInTheDocument();
  });

  it('stays on step 3 with a banner when starting fails, and keeps the depth', async () => {
    stepThree(settingsReply(), crawlStart(500, { code: 'x' }));
    await waitFor(() => expect(screen.getByTestId('wizard-depth')).toHaveAttribute('max', '4'));
    fireEvent.change(screen.getByTestId('wizard-depth'), { target: { value: '3' } });
    fireEvent.click(screen.getByTestId('wizard-finish'));
    expect(await screen.findByTestId('wizard-done-error')).toHaveTextContent(
      'could not start the crawl',
    );
    expect(screen.getByTestId('wizard-depth')).toHaveValue('3');
    expect(screen.getByTestId('wizard-finish')).not.toBeDisabled();
  });

  it('reports a network failure of the start call', async () => {
    stepThree(
      settingsReply(),
      http.put('*/me/crawl/settings', () => HttpResponse.error()),
    );
    fireEvent.click(await screen.findByTestId('wizard-finish'));
    expect(await screen.findByTestId('wizard-done-error')).toBeInTheDocument();
  });

  it('re-reads the settings and explains depth_above_cap', async () => {
    let reads = 0;
    stepThree(
      http.get('*/me/crawl/settings', () => {
        reads += 1;
        return HttpResponse.json({ depth: 2, depth_default: 2, depth_max: reads === 1 ? 4 : 2 });
      }),
      crawlStart(422, { code: 'depth_above_cap' }),
    );
    await waitFor(() => expect(screen.getByTestId('wizard-depth')).toHaveAttribute('max', '4'));
    fireEvent.change(screen.getByTestId('wizard-depth'), { target: { value: '4' } });
    fireEvent.click(screen.getByTestId('wizard-finish'));
    expect(await screen.findByTestId('wizard-done-error')).toHaveTextContent('above the limit');
    await waitFor(() => expect(screen.getByTestId('wizard-depth')).toHaveAttribute('max', '2'));
    expect(reads).toBe(2);
  });

  it('works with depth 2 and a stale banner when the settings fail, and retries them', async () => {
    let fail = true;
    stepThree(
      http.get('*/me/crawl/settings', () =>
        fail
          ? HttpResponse.json({ code: 'x' }, { status: 500 })
          : HttpResponse.json({ depth: 2, depth_default: 2, depth_max: 4 }),
      ),
      crawlStart(200, { depth: 2, depth_default: 2, depth_max: 4 }),
    );
    const banner = await screen.findByTestId('wizard-stale-banner');
    expect(banner).toHaveTextContent('Depth 2 is used');
    expect(screen.getByTestId('wizard-depth')).toHaveValue('2');
    fail = false;
    fireEvent.click(screen.getByTestId('wizard-stale-retry'));
    await waitFor(() => expect(screen.queryByTestId('wizard-stale-banner')).toBeNull());
    expect(screen.getByTestId('wizard-depth')).toHaveAttribute('max', '4');
  });

  it('shows the Vietnamese copy for step 3', async () => {
    stepThree(settingsReply());
    await screen.findByTestId('wizard-step-done');
    fireEvent.click(screen.getByTestId('shell-lang-toggle'));
    await waitFor(() =>
      expect(screen.getByTestId('wizard-finish')).toHaveTextContent(
        'Bắt đầu thu thập và về Trang chủ',
      ),
    );
    expect(screen.getByTestId('wizard-depth-value')).toHaveTextContent('Độ sâu thu thập: 2');
  });
});

describe('Add API key modal', () => {
  const newKey = { ...acceptedKey, id: 6, label: 'Second key' };
  const openModal = async () => {
    server.use(keysReply([acceptedKey]), accountsReply([account]), settingsReply());
    open();
    const opener = await screen.findByTestId('wizard-add-key');
    opener.focus();
    fireEvent.click(opener);
    return { opener, dialog: await screen.findByTestId('wizard-addkey-modal') };
  };
  const fillModal = (label = 'Second key', key = KEY, secret = SECRET) => {
    fireEvent.change(screen.getByTestId('wizard-addkey-label'), { target: { value: label } });
    fireEvent.change(screen.getByTestId('wizard-addkey-key'), { target: { value: key } });
    fireEvent.change(screen.getByTestId('wizard-addkey-secret'), { target: { value: secret } });
  };

  it('opens as a labelled modal dialog with focus inside, and Esc restores focus', async () => {
    const { opener, dialog } = await openModal();
    expect(dialog).toHaveAttribute('role', 'dialog');
    expect(dialog).toHaveAttribute('aria-modal', 'true');
    expect(dialog).toHaveAccessibleName('Add API key');
    expect(screen.getByTestId('wizard-addkey-label')).toHaveFocus();

    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByTestId('wizard-addkey-modal')).toBeNull();
    expect(opener).toHaveFocus();
  });

  it('closes on Cancel and traps Tab inside the dialog', async () => {
    await openModal();
    const cancel = screen.getByTestId('wizard-addkey-cancel');
    const label = screen.getByTestId('wizard-addkey-label');
    const submit = screen.getByTestId('wizard-addkey-submit');
    submit.focus();
    fireEvent.keyDown(document, { key: 'Tab' });
    expect(label).toHaveFocus();
    fireEvent.keyDown(document, { key: 'Tab', shiftKey: true });
    expect(submit).toHaveFocus();
    fireEvent.click(cancel);
    expect(screen.queryByTestId('wizard-addkey-modal')).toBeNull();
  });

  it('toggles the secret and validates the label (1 to 80), key and secret on submit', async () => {
    await openModal();
    const secret = screen.getByTestId('wizard-addkey-secret');
    expect(secret).toHaveAttribute('type', 'password');
    fireEvent.click(screen.getByTestId('wizard-addkey-secret-toggle'));
    expect(secret).toHaveAttribute('type', 'text');
    expect(screen.getByTestId('wizard-addkey-secret-toggle')).toHaveAttribute(
      'aria-pressed',
      'true',
    );

    fireEvent.click(screen.getByTestId('wizard-addkey-submit'));
    expect(screen.getByText('Enter a label of 1 to 80 characters.')).toBeInTheDocument();
    expect(screen.getByTestId('wizard-addkey-label')).toHaveFocus();

    fillModal('x'.repeat(81), 'nope');
    fireEvent.click(screen.getByTestId('wizard-addkey-submit'));
    expect(screen.getByText('Enter a label of 1 to 80 characters.')).toBeInTheDocument();

    fillModal('Second key', 'nope');
    fireEvent.click(screen.getByTestId('wizard-addkey-submit'));
    expect(screen.getByTestId('wizard-addkey-key')).toHaveFocus();
    expect(screen.getByText(/The key should be 32 characters/)).toBeInTheDocument();

    fillModal('Second key', KEY, 'bad');
    fireEvent.click(screen.getByTestId('wizard-addkey-submit'));
    expect(screen.getByTestId('wizard-addkey-secret')).toHaveFocus();
    expect(screen.getByText(/The secret should be 16 characters/)).toBeInTheDocument();
  });

  it('adds the key, closes, and returns to step 2 with the new key selected', async () => {
    const create = vi.fn(async ({ request }: { request: Request }) => {
      expect(await request.json()).toEqual({ label: 'Second key', key: KEY, secret: SECRET });
      return HttpResponse.json(newKey, { status: 201 });
    });
    await openModal();
    let added = false;
    server.use(
      http.get('*/keys', () => HttpResponse.json(added ? [acceptedKey, newKey] : [acceptedKey])),
      http.post('*/keys', (info) => {
        added = true;
        return create(info);
      }),
    );
    fillModal();
    fireEvent.click(screen.getByTestId('wizard-addkey-submit'));

    expect(await screen.findByTestId('wizard-step-connect')).toBeInTheDocument();
    expect(screen.queryByTestId('wizard-addkey-modal')).toBeNull();
    expect(screen.getByTestId('wizard-live-region')).toHaveTextContent('Key added');
    expect(screen.getByTestId('wizard-key-select')).toHaveValue('6');
  });

  it.each([
    [422, { code: 'key_invalid' }, 'wizard-addkey-error', 'Flickr rejected this key'],
    [503, { code: 'no_node_available' }, 'wizard-addkey-error', 'No healthy node'],
    [500, { code: 'x' }, 'wizard-addkey-error', 'could not reach Flickr'],
  ])('shows status %s as a banner and keeps the values', async (status, body, testId, text) => {
    server.use(http.post('*/keys', () => HttpResponse.json(body, { status })));
    await openModal();
    fillModal();
    fireEvent.click(screen.getByTestId('wizard-addkey-submit'));
    expect(await screen.findByTestId(testId)).toHaveTextContent(text);
    expect(screen.getByTestId(testId)).toHaveFocus();
    expect(screen.getByTestId('wizard-addkey-key')).toHaveValue(KEY);
  });

  it('shows key_duplicate and server field errors under the fields', async () => {
    server.use(
      http.post('*/keys', () => HttpResponse.json({ code: 'key_duplicate' }, { status: 409 })),
    );
    await openModal();
    fillModal();
    fireEvent.click(screen.getByTestId('wizard-addkey-submit'));
    expect(await screen.findByText('This key is already in your account.')).toBeInTheDocument();
    expect(screen.getByTestId('wizard-addkey-key')).toHaveFocus();

    server.use(
      http.post('*/keys', () =>
        HttpResponse.json(
          { code: 'validation_failed', details: { fields: { secret: 'bad' } } },
          { status: 400 },
        ),
      ),
    );
    fireEvent.change(screen.getByTestId('wizard-addkey-key'), { target: { value: KEY } });
    fireEvent.click(screen.getByTestId('wizard-addkey-submit'));
    expect(await screen.findByText(/The secret should be 16 characters/)).toBeInTheDocument();
    expect(screen.getByTestId('wizard-addkey-secret')).toHaveFocus();
  });
});

describe('setup wizard step 1 extras', () => {
  it('shows the Checking with Flickr status while sending', async () => {
    server.use(
      keysReply(),
      accountsReply(),
      http.post('*/keys', async () => {
        await delay(60);
        return HttpResponse.json(acceptedKey, { status: 201 });
      }),
    );
    open();
    await fillAndSubmit();
    expect(await screen.findByTestId('wizard-key-checking')).toHaveTextContent(
      'Checking with Flickr…',
    );
    expect(await screen.findByTestId('wizard-step-connect')).toBeInTheDocument();
  });

  it('fills a random valid-looking pair in the demo build without any call', async () => {
    const create = vi.fn(() => HttpResponse.json(acceptedKey, { status: 201 }));
    server.use(keysReply(), accountsReply(), http.post('*/keys', create));
    open();
    fireEvent.click(await screen.findByTestId('wizard-key-fill'));
    expect((screen.getByTestId('wizard-key-input') as HTMLInputElement).value).toMatch(
      /^[a-f\d]{32}$/,
    );
    expect((screen.getByTestId('wizard-key-secret') as HTMLInputElement).value).toMatch(
      /^[a-f\d]{16}$/,
    );
    expect(create).not.toHaveBeenCalled();
    fireEvent.click(screen.getByTestId('wizard-key-submit'));
    expect(await screen.findByTestId('wizard-step-connect')).toBeInTheDocument();
  });
});
