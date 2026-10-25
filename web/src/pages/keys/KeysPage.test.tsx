import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw/http';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { renderApp, userSession } from '../../test/render';
import { server } from '../../test/server';

const leaveTo = vi.hoisted(() => vi.fn());
vi.mock('./navigate', () => ({ leaveTo }));

const KEY = '0123456789abcdef0123456789abcdef';
const SECRET = '0123456789abcdef';

const apiKey = (id: number, extra: Record<string, unknown> = {}) => ({
  id,
  label: `Key ${String(id)}`,
  key_public: '012345••••',
  per_hour_limit: 3600,
  per_second_limit: 4,
  burst: 8,
  status: 'valid',
  created_at: '2026-10-25T00:00:00Z',
  ...extra,
});

/** One `GET /keys/{id}/quota` answer; the page builds its usage from these. */
const usageItem = (id: number, extra: Record<string, unknown> = {}) => ({
  key_id: id,
  used: 2140,
  limit: 3600,
  reserve: 100,
  reset_at: new Date(Date.now() + 20 * 60_000).toISOString(),
  state: 'ok',
  lanes: { interactive: 0, person: 1500, photo: 640 },
  ...extra,
});

const quotaReply = (items: { key_id?: unknown }[]) =>
  http.get('*/keys/:id/quota', ({ params }) => {
    const item = items.find((candidate) => String(candidate.key_id) === params.id);
    return item === undefined
      ? HttpResponse.json({ code: 'not_found' }, { status: 404 })
      : HttpResponse.json(item);
  });

const account = (extra: Record<string, unknown> = {}) => ({
  id: 9,
  key_id: 1,
  nsid: '123@N01',
  username: 'Anna',
  status: 'ok',
  created_at: '2026-10-25T00:00:00Z',
  ...extra,
});

function world(keys: unknown[], usage: unknown[] = [], accounts: unknown[] = []) {
  server.use(
    http.get('*/keys', () => HttpResponse.json(keys)),
    quotaReply(usage as { key_id?: unknown }[]),
    http.get('*/flickr-accounts', () => HttpResponse.json(accounts)),
  );
}

const open = (path = '/keys', locale: 'en' | 'vi' = 'en') =>
  renderApp({ session: userSession, path, locale });
const changeValue = (testId: string, value: string) =>
  fireEvent.change(screen.getByTestId(testId), { target: { value } });

async function fillAndSubmit() {
  changeValue('keys-add-label', 'Mine');
  changeValue('keys-add-key', KEY);
  changeValue('keys-add-secret', SECRET);
  fireEvent.click(screen.getByTestId('keys-add-submit'));
}

beforeEach(() => {
  leaveTo.mockReset();
});

describe('KeysPage states', () => {
  it('shows the connected account and removes the success parameter', async () => {
    world([apiKey(1)], [usageItem(1)], [account()]);
    const { router } = open('/keys?connected=1');

    expect(await screen.findByTestId('keys-connect-notice')).toHaveTextContent(
      'Anna is connected. Crawl can start.',
    );
    await waitFor(() => expect(router.state.location.search).toBe(''));
  });

  it('shows a specific callback error and retries authorization', async () => {
    leaveTo.mockImplementation(() => undefined);
    world([apiKey(1)], [usageItem(1)], []);
    server.use(
      http.post('*/flickr-accounts/connect', () =>
        HttpResponse.json({ authorize_url: 'https://flickr.test/authorize' }),
      ),
    );
    const { router } = open('/keys?connect_error=invalid_state');

    expect(await screen.findByTestId('keys-connect-notice')).toHaveTextContent(
      'The Flickr sign-in expired. Start again.',
    );
    await screen.findByTestId('keys-accounts-none');
    fireEvent.click(screen.getByTestId('keys-connect-retry'));
    await waitFor(() => expect(leaveTo).toHaveBeenCalledWith('https://flickr.test/authorize'));
    await waitFor(() => expect(router.state.location.search).toBe(''));
  });

  it('shows the empty block and opens the Add modal from it', async () => {
    world([]);
    open();

    expect(await screen.findByTestId('keys-empty')).toHaveTextContent('No API key yet');
    expect(screen.queryByTestId('keys-note')).toBeNull();
    expect(document.title).toBe('Keys & quota · XFlickr');
    screen.getByTestId('keys-empty-cta').focus();
    fireEvent.click(screen.getByTestId('keys-empty-cta'));
    expect(screen.getByRole('dialog')).toBeInTheDocument();
    expect(screen.getByTestId('keys-add-label')).toHaveFocus();
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByTestId('keys-add-modal')).toBeNull();
    expect(screen.getByTestId('keys-empty-cta')).toHaveFocus();
  });

  it('shows the error block and retries the list', async () => {
    let calls = 0;
    server.use(
      http.get('*/keys', () => {
        calls += 1;
        return calls === 1 ? new HttpResponse(null, { status: 500 }) : HttpResponse.json([]);
      }),
    );
    open();

    expect(await screen.findByTestId('keys-error')).toHaveTextContent('Keys could not load');
    fireEvent.click(screen.getByTestId('keys-error-retry'));
    expect(await screen.findByTestId('keys-empty')).toBeInTheDocument();
  });

  it('shows the forbidden block with a link home on 403', async () => {
    server.use(http.get('*/keys', () => HttpResponse.json({ code: 'forbidden' }, { status: 403 })));
    open();

    expect(await screen.findByTestId('keys-forbidden')).toBeInTheDocument();
    expect(screen.getByTestId('keys-forbidden-cta')).toHaveAttribute('href', '/');
    expect(screen.queryByTestId('keys-error-retry')).toBeNull();
  });

  it('shows the skeleton after the delay while the list loads', async () => {
    server.use(
      http.get('*/keys', async () => {
        await new Promise((resolve) => setTimeout(resolve, 400));
        return HttpResponse.json([]);
      }),
    );
    open();

    expect(await screen.findByTestId('keys-skeleton')).toHaveAttribute('aria-busy', 'true');
    expect(await screen.findByTestId('keys-empty')).toBeInTheDocument();
  });

  it('keeps the cards and warns when usage fails, then recovers on retry', async () => {
    let usageCalls = 0;
    world([apiKey(1)], [], []);
    server.use(
      http.get('*/keys/:id/quota', () => {
        usageCalls += 1;
        return usageCalls === 1
          ? new HttpResponse(null, { status: 500 })
          : HttpResponse.json(usageItem(1));
      }),
    );
    open();

    expect(await screen.findByTestId('keys-stale-banner')).toBeInTheDocument();
    expect(screen.getByTestId('keys-card-used')).toHaveTextContent('Used – / 3,600');
    expect(screen.getByTestId('keys-card-bar')).not.toHaveAttribute('aria-valuetext');
    fireEvent.click(screen.getByTestId('keys-stale-retry'));
    await waitFor(() =>
      expect(screen.getByTestId('keys-card-used')).toHaveTextContent('Used 2,140 / 3,600'),
    );
    expect(screen.queryByTestId('keys-stale-banner')).toBeNull();
  });

  it('shows an inline retry when accounts fail to load', async () => {
    world([apiKey(1)], [usageItem(1)]);
    let failing = true;
    server.use(
      http.get('*/flickr-accounts', () =>
        failing ? new HttpResponse(null, { status: 500 }) : HttpResponse.json([account()]),
      ),
    );
    open();

    const retry = await screen.findByTestId('keys-accounts-retry');
    failing = false;
    fireEvent.click(retry);
    expect(await screen.findByTestId('keys-account-123@N01')).toBeInTheDocument();
  });
});

describe('KeysPage cards', () => {
  it('renders a mixed page with every state, hint and masked key', async () => {
    world(
      [
        apiKey(1),
        apiKey(2),
        apiKey(3),
        apiKey(4, { status: 'revoked' }),
        apiKey(5, { status: 'invalid' }),
      ],
      [
        usageItem(1),
        usageItem(2, { used: 3550, state: 'cooling' }),
        usageItem(3, { used: 3600, state: 'exhausted' }),
        usageItem(5),
      ],
    );
    open();

    const card = async (id: number) => within(await screen.findByTestId(`keys-card-${String(id)}`));
    const ok = await card(1);
    expect(ok.getByTestId('keys-card-state')).toHaveTextContent('OK');
    expect(ok.getByTestId('keys-card-masked')).toHaveTextContent('012345••••');
    expect(ok.getByTestId('keys-card-bar')).toHaveAttribute(
      'aria-valuetext',
      '2,140 of 3,600 calls used',
    );
    expect(ok.getByTestId('keys-card-reserve')).toHaveTextContent(
      '100 calls for actions you start',
    );
    expect(ok.getByTestId('keys-card-burst')).toHaveTextContent('8');
    expect(ok.getByTestId('keys-card-reset')).toHaveTextContent(/^\d\d:\d\d$/);
    expect(ok.getByTestId('keys-card-lane-person')).toHaveTextContent('1,500');
    expect(ok.getByTestId('keys-card-lane-sizes')).toHaveTextContent('640');
    expect(ok.queryByTestId('keys-card-hint')).toBeNull();
    expect((await card(2)).getByTestId('keys-card-hint')).toHaveTextContent('Cap reached');
    expect((await card(3)).getByTestId('keys-card-hint')).toHaveTextContent('Cap reached');
    const revoked = await card(4);
    expect(revoked.getByTestId('keys-card-state')).toHaveTextContent('Revoked');
    expect(revoked.getByTestId('keys-card-hint')).toHaveTextContent('no longer accepts');
    const invalid = await card(5);
    expect(invalid.getByTestId('keys-card-state')).toHaveTextContent('Invalid');
    expect(screen.getByTestId('keys-note')).toBeInTheDocument();
    expect(screen.getAllByRole('article')).toHaveLength(5);
  });

  it('lists accounts per key with Reconnect for those that need it', async () => {
    world(
      [apiKey(1), apiKey(2)],
      [usageItem(1), usageItem(2)],
      [
        account(),
        account({ id: 10, nsid: '9@N02', username: 'Bao', status: 'reconnect_required' }),
        account({ id: 11, nsid: '8@N03', username: 'Cuc', status: 'revoked' }),
      ],
    );
    leaveTo.mockImplementation(() => undefined);
    server.use(
      http.post('*/flickr-accounts/connect', () =>
        HttpResponse.json({ authorize_url: 'https://flickr.test/authorize' }),
      ),
    );
    open();

    expect(await screen.findByTestId('keys-account-123@N01')).toHaveTextContent('Anna');
    expect(screen.queryByTestId('keys-account-reconnect-123@N01')).toBeNull();
    expect(screen.getByTestId('keys-account-status-9@N02')).toHaveTextContent('Reconnect needed');
    expect(screen.getByTestId('keys-account-status-8@N03')).toHaveTextContent('Access revoked');
    expect(
      within(screen.getByTestId('keys-accounts-2')).getByTestId('keys-accounts-none'),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByTestId('keys-account-reconnect-9@N02'));
    await waitFor(() => expect(leaveTo).toHaveBeenCalledWith('https://flickr.test/authorize'));
  });

  it('starts the connection from a key without accounts and reports a failure', async () => {
    world([apiKey(2)], [usageItem(2)]);
    server.use(
      http.post('*/flickr-accounts/connect', () =>
        HttpResponse.json({ code: 'key_not_valid' }, { status: 409 }),
      ),
    );
    open();

    fireEvent.click(await screen.findByTestId('keys-accounts-connect-2'));
    expect(await screen.findByTestId('keys-toast')).toHaveTextContent('Could not start');
    expect(leaveTo).not.toHaveBeenCalled();
  });

  it('removes a key after confirmation and moves focus to the empty block', async () => {
    world([apiKey(1)], [usageItem(1)], [account()]);
    let deletes = 0;
    server.use(
      http.delete('*/keys/1', () => {
        deletes += 1;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    open();

    fireEvent.click(await screen.findByTestId('keys-card-remove'));
    expect(screen.getByTestId('keys-remove-modal')).toHaveTextContent('1 Flickr account');
    expect(screen.getByTestId('keys-remove-cancel')).toHaveFocus();
    fireEvent.click(screen.getByTestId('keys-remove-cancel'));
    expect(screen.queryByTestId('keys-remove-modal')).toBeNull();
    expect(deletes).toBe(0);

    fireEvent.click(screen.getByTestId('keys-card-remove'));
    server.use(http.get('*/keys', () => HttpResponse.json([])));
    fireEvent.click(screen.getByTestId('keys-remove-confirm'));
    expect(await screen.findByTestId('keys-empty')).toBeInTheDocument();
    expect(deletes).toBe(1);
    await waitFor(() => expect(screen.getByTestId('keys-empty-cta')).toHaveFocus());
  });

  it('treats a 404 on remove as success and reports other failures', async () => {
    world([apiKey(1), apiKey(2)], [usageItem(1), usageItem(2)]);
    server.use(
      http.delete('*/keys/1', () => HttpResponse.json({ code: 'not_found' }, { status: 404 })),
      http.delete('*/keys/2', () => HttpResponse.json({ code: 'x' }, { status: 500 })),
    );
    open();

    fireEvent.click(await screen.findByLabelText('Remove Key 2'));
    fireEvent.click(screen.getByTestId('keys-remove-confirm'));
    expect(await screen.findByTestId('keys-toast')).toHaveTextContent('Nothing changed');
    fireEvent.click(screen.getByTestId('keys-remove-cancel'));

    fireEvent.click(screen.getByLabelText('Remove Key 1'));
    fireEvent.click(screen.getByTestId('keys-remove-confirm'));
    await waitFor(() => expect(screen.queryByTestId('keys-card-1')).toBeNull());
    expect(screen.getByTestId('keys-card-2')).toBeInTheDocument();
  });

  it('announces a state change once through the polite region', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      let state = 'ok';
      world([apiKey(1)], []);
      server.use(http.get('*/keys/:id/quota', () => HttpResponse.json(usageItem(1, { state }))));
      open();

      await screen.findByTestId('keys-card-1');
      expect(screen.getByTestId('keys-live-region')).toBeEmptyDOMElement();
      state = 'exhausted';
      await vi.advanceTimersByTimeAsync(10_500);
      await waitFor(() =>
        expect(screen.getByTestId('keys-live-region')).toHaveTextContent('Key 1 is now Exhausted.'),
      );
    } finally {
      vi.useRealTimers();
    }
  });

  it('fetches usage again when a countdown reaches zero', async () => {
    let calls = 0;
    world([apiKey(1)], []);
    server.use(
      http.get('*/keys/:id/quota', () => {
        calls += 1;
        return HttpResponse.json(usageItem(1, { reset_at: '2000-01-01T00:00:00Z' }));
      }),
    );
    open();

    await screen.findByTestId('keys-card-1');
    await waitFor(() => expect(screen.getByTestId('keys-card-reset')).toHaveTextContent('00:00'));
    await waitFor(() => expect(calls).toBeGreaterThanOrEqual(2));
  });

  it('renders Vietnamese copy', async () => {
    world([apiKey(1)], [usageItem(1, { state: 'exhausted' })]);
    open('/keys', 'vi');

    expect(
      await screen.findByRole('heading', { level: 1, name: 'Khóa & hạn mức' }),
    ).toBeInTheDocument();
    expect(await screen.findByTestId('keys-card-state')).toHaveTextContent('Hết hạn mức');
    expect(screen.getByTestId('keys-card-used')).toHaveTextContent('Đã dùng 2.140 / 3.600');
  });
});

describe('Add API key', () => {
  it('opens from ?add=1, validates, adds the key, and removes the query', async () => {
    world([]);
    const posted: unknown[] = [];
    server.use(
      http.post('*/keys', async ({ request }) => {
        posted.push(await request.json());
        return HttpResponse.json(apiKey(7, { label: 'Mine' }), { status: 201 });
      }),
    );
    const { router } = open('/keys?add=1');

    expect(await screen.findByTestId('keys-add-modal')).toBeInTheDocument();
    fireEvent.click(screen.getByTestId('keys-add-submit'));
    expect(screen.getByTestId('keys-add-error-label')).toHaveTextContent('Enter a label');
    expect(screen.getByTestId('keys-add-label')).toHaveFocus();
    changeValue('keys-add-label', 'Mine');
    fireEvent.click(screen.getByTestId('keys-add-submit'));
    expect(screen.getByTestId('keys-add-error-key')).toHaveTextContent('32 characters');
    expect(screen.getByTestId('keys-add-key')).toHaveFocus();
    changeValue('keys-add-key', KEY);
    fireEvent.click(screen.getByTestId('keys-add-submit'));
    expect(screen.getByTestId('keys-add-error-secret')).toHaveTextContent('16 characters');
    expect(screen.getByTestId('keys-add-secret')).toHaveFocus();
    changeValue('keys-add-secret', ` ${SECRET.slice(0, 8)}\n${SECRET.slice(8)} `);

    const toggle = screen.getByTestId('keys-add-secret-toggle');
    expect(screen.getByTestId('keys-add-secret')).toHaveAttribute('type', 'password');
    fireEvent.click(toggle);
    expect(screen.getByTestId('keys-add-secret')).toHaveAttribute('type', 'text');
    expect(toggle).toHaveAttribute('aria-pressed', 'true');

    server.use(
      http.get('*/keys', () => HttpResponse.json([apiKey(7, { label: 'Mine' })])),
      quotaReply([usageItem(7, { used: 0 })]),
    );
    fireEvent.click(screen.getByTestId('keys-add-submit'));

    await waitFor(() => expect(screen.queryByTestId('keys-add-modal')).toBeNull());
    expect(posted).toEqual([{ label: 'Mine', key: KEY, secret: SECRET }]);
    expect(await screen.findByTestId('keys-toast')).toHaveTextContent('Key checked and saved');
    expect(await screen.findByTestId('keys-card-7')).toBeInTheDocument();
    expect(screen.getByRole('heading', { level: 3, name: 'Mine' })).toHaveFocus();
    expect(router.state.location.search).toBe('');
  });

  it.each([
    [422, { code: 'key_invalid' }, 'keys-add-error', 'Flickr did not accept'],
    [503, { code: 'no_node_available' }, 'keys-add-error', 'No healthy node'],
    [429, { code: 'rate_limited' }, 'keys-add-error', 'We could not add the key'],
    [409, { code: 'key_duplicate' }, 'keys-add-error-key', 'already added'],
    [
      400,
      { code: 'validation_failed', details: { fields: { secret: 'invalid_format' } } },
      'keys-add-error-secret',
      '16 characters',
    ],
  ])('keeps the modal open on %i', async (status, body, testId, text) => {
    world([apiKey(1)], [usageItem(1)]);
    server.use(http.post('*/keys', () => HttpResponse.json(body, { status })));
    open();

    fireEvent.click(await screen.findByTestId('keys-add'));
    await fillAndSubmit();

    expect(await screen.findByTestId(testId)).toHaveTextContent(text);
    expect(screen.getByTestId('keys-add-modal')).toBeInTheDocument();
    expect(screen.getByTestId('keys-add-label')).toHaveValue('Mine');
    expect(screen.getByTestId('keys-add-submit')).toBeEnabled();
  });

  it('closes with Cancel and keeps nothing', async () => {
    world([apiKey(1)], [usageItem(1)]);
    open();

    (await screen.findByTestId('keys-add')).focus();
    fireEvent.click(screen.getByTestId('keys-add'));
    changeValue('keys-add-label', 'Typed');
    fireEvent.click(screen.getByTestId('keys-add-cancel'));
    expect(screen.queryByTestId('keys-add-modal')).toBeNull();
    expect(screen.getByTestId('keys-add')).toHaveFocus();
    fireEvent.click(screen.getByTestId('keys-add'));
    expect(screen.getByTestId('keys-add-label')).toHaveValue('');
  });

  it('traps Tab inside the dialog', async () => {
    world([]);
    open();

    fireEvent.click(await screen.findByTestId('keys-empty-cta'));
    const cancel = screen.getByTestId('keys-add-cancel');
    const submit = screen.getByTestId('keys-add-submit');
    submit.focus();
    fireEvent.keyDown(document, { key: 'Tab' });
    expect(screen.getByTestId('keys-add-label')).toHaveFocus();
    fireEvent.keyDown(document, { key: 'Tab', shiftKey: true });
    expect(submit).toHaveFocus();
    expect(cancel).toBeInTheDocument();
  });
});
