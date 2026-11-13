import { act, fireEvent, screen } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { ApiKey } from '../../shared/api/gen/models';
import { server } from '../../test/server';
import { renderApp, userSession } from '../../test/render';

const KEY = 'a'.repeat(32);
const SECRET = 'b'.repeat(16);
const key = {
  id: 5,
  label: 'Existing key',
  key_public: 'abcd••••',
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

type Deferred = { promise: Promise<void>; resolve: () => void };

function deferred(): Deferred {
  let resolve!: () => void;
  const promise = new Promise<void>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

function setupReplies(accounts = [account]) {
  server.use(
    http.get('*/keys', () => HttpResponse.json([key])),
    http.get('*/flickr-accounts', () => HttpResponse.json(accounts)),
    http.get('*/me/crawl/settings', () =>
      HttpResponse.json({ depth: 2, depth_default: 2, depth_max: 4 }),
    ),
    http.get('*/me/home', () => new HttpResponse(null, { status: 401 })),
  );
}

function finishDeferred(request: Deferred) {
  return act(async () => {
    request.resolve();
    await request.promise;
    await new Promise((resolve) => window.setTimeout(resolve, 0));
  });
}

afterEach(() => {
  server.resetHandlers();
  vi.unstubAllGlobals();
});

describe('stale setup page reports', () => {
  it('ignores key save success after the key page rolls back to done', async () => {
    const request = deferred();
    const started = deferred();
    setupReplies();
    server.use(
      http.post('*/keys', async () => {
        started.resolve();
        await request.promise;
        return HttpResponse.json({ ...key, id: 6 }, { status: 201 });
      }),
    );
    const app = renderApp({ session: userSession, path: '/onboarding' });
    const saved = deferred();
    let unsubscribe: () => void = () => {};
    unsubscribe = app.client.getQueryCache().subscribe(() => {
      if (app.client.getQueryData<ApiKey[]>(['/keys'])?.some((item) => item.id === 6)) {
        unsubscribe();
        saved.resolve();
      }
    });
    await screen.findByTestId('wizard-step-done');
    fireEvent.click(screen.getByTestId('wizard-done-back'));
    fireEvent.click(await screen.findByTestId('wizard-connect-back'));
    fireEvent.change(await screen.findByTestId('wizard-key-input'), { target: { value: KEY } });
    fireEvent.change(screen.getByTestId('wizard-key-secret'), { target: { value: SECRET } });
    fireEvent.click(screen.getByTestId('wizard-key-submit'));
    await started.promise;
    fireEvent.click(screen.getByTestId('wizard-key-back'));
    await screen.findByTestId('wizard-step-done');

    await finishDeferred(request);
    await saved.promise;
    await act(async () => Promise.resolve());

    expect(app.router.state.location.pathname).toBe('/onboarding');
    expect(screen.getByTestId('wizard-step-done')).toBeInTheDocument();
    expect(screen.queryByTestId('home-toast')).not.toBeInTheDocument();
  });

  it('ignores crawl success after the user rolls back from done', async () => {
    const request = deferred();
    const started = deferred();
    setupReplies();
    server.use(
      http.put('*/me/crawl/settings', async () => {
        started.resolve();
        await request.promise;
        return HttpResponse.json({ depth: 2, depth_default: 2, depth_max: 4 });
      }),
    );
    const app = renderApp({ session: userSession, path: '/onboarding' });
    await screen.findByTestId('wizard-step-done');
    fireEvent.click(screen.getByTestId('wizard-finish'));
    await started.promise;
    fireEvent.click(screen.getByTestId('wizard-done-back'));
    await screen.findByTestId('wizard-step-connect');

    await finishDeferred(request);
    await act(async () => {
      await new Promise((resolve) => window.setTimeout(resolve, 0));
    });

    expect(app.router.state.location.pathname).toBe('/onboarding');
    expect(screen.getByTestId('wizard-step-connect')).toBeInTheDocument();
    expect(screen.queryByTestId('wizard-connect-waiting')).not.toBeInTheDocument();
  });

  it('does not redirect after a canceled authorization resolves late', async () => {
    const request = deferred();
    const started = deferred();
    const assign = vi.fn();
    vi.stubGlobal('location', { ...window.location, assign });
    setupReplies([]);
    server.use(
      http.post('*/flickr-accounts/connect', async () => {
        started.resolve();
        await request.promise;
        return HttpResponse.json({ authorize_url: 'https://flickr.test/authorize' });
      }),
    );
    const app = renderApp({ session: userSession, path: '/onboarding' });
    fireEvent.click(await screen.findByTestId('wizard-connect-allow'));
    await started.promise;
    fireEvent.click(screen.getByTestId('wizard-connect-cancel'));

    await finishDeferred(request);

    expect(assign).not.toHaveBeenCalled();
    expect(app.router.state.location.pathname).toBe('/onboarding');
    expect(screen.getByTestId('wizard-step-connect')).toBeInTheDocument();
  });
});
