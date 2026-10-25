import { http, HttpResponse } from 'msw/http';
import { afterEach, describe, expect, it } from 'vitest';
import { server } from '../../test/server';
import { SESSION_EXPIRED_EVENT } from './sessionEvents';
import { csrfFetch } from './mutator';

afterEach(() => {
  document.cookie = 'xf_csrf=; Max-Age=0; path=/';
});

describe('csrfFetch', () => {
  it('sends the readable cookie as a header on unsafe calls with same-origin credentials', async () => {
    document.cookie = 'xf_csrf=token-1; path=/';
    server.use(
      http.post('*/mutator-test', ({ request }) => {
        expect(request.headers.get('X-CSRF-Token')).toBe('token-1');
        expect(request.credentials).toBe('same-origin');
        return HttpResponse.json({ ok: true });
      }),
    );

    await expect(csrfFetch('/mutator-test', { method: 'POST' })).resolves.toMatchObject({
      status: 200,
    });
  });

  it('does not send a CSRF header on safe calls', async () => {
    document.cookie = 'xf_csrf=token-1; path=/';
    server.use(
      http.get('*/mutator-test', ({ request }) => {
        expect(request.headers.has('X-CSRF-Token')).toBe(false);
        return HttpResponse.json({ ok: true });
      }),
    );

    await csrfFetch('/mutator-test', { method: 'GET' });
  });

  it('refreshes the cookie and retries one csrf_rejected response exactly once', async () => {
    document.cookie = 'xf_csrf=expired; path=/';
    let calls = 0;
    server.use(
      http.get('*/me', () => HttpResponse.json({})),
      http.post('*/mutator-test', ({ request }) => {
        calls += 1;
        if (calls === 1) {
          expect(request.headers.get('X-CSRF-Token')).toBe('expired');
          document.cookie = 'xf_csrf=fresh; path=/';
          return HttpResponse.json(
            { code: 'csrf_rejected', message: 'CSRF token is missing or invalid' },
            { status: 403 },
          );
        }
        expect(request.headers.get('X-CSRF-Token')).toBe('fresh');
        return HttpResponse.json({ ok: true });
      }),
    );

    await expect(csrfFetch('/mutator-test', { method: 'POST' })).resolves.toMatchObject({
      status: 200,
    });
    expect(calls).toBe(2);
  });

  it('does not retry other 403 responses and signals 401 responses', async () => {
    let forbiddenCalls = 0;
    server.use(
      http.delete('*/mutator-test', () => {
        forbiddenCalls += 1;
        return HttpResponse.json({ code: 'forbidden' }, { status: 403 });
      }),
      http.get('*/mutator-test', () =>
        HttpResponse.json({ code: 'unauthorized' }, { status: 401 }),
      ),
    );
    let expiredCount = 0;
    const onExpired = () => {
      expiredCount += 1;
    };
    window.addEventListener(SESSION_EXPIRED_EVENT, onExpired);

    await csrfFetch('/mutator-test', { method: 'DELETE' });
    await csrfFetch('/mutator-test', { method: 'GET' });

    expect(forbiddenCalls).toBe(1);
    expect(expiredCount).toBe(1);
    window.removeEventListener(SESSION_EXPIRED_EVENT, onExpired);
  });
});
