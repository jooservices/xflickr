import { http, HttpResponse } from 'msw/http';
import { crawlHandlers } from './crawl';
import { authenticationOptions, sessionCollectionEnvelope } from './rest';

export const handlers = [
  ...crawlHandlers,
  http.get('http://localhost/api/health', () =>
    HttpResponse.json({ status: 'ok', version: 'test' }),
  ),
  http.get('*/healthz', () => HttpResponse.json({ status: 'ok', version: 'test' })),
  http.get('*/api/v1/authentication-options', () => HttpResponse.json(authenticationOptions())),
  http.get('*/api/v1/me/sessions', () => HttpResponse.json(sessionCollectionEnvelope([]))),
  http.get('*/keys', () => HttpResponse.json([])),
  http.get('*/flickr-accounts', () => HttpResponse.json([])),
  http.get('*/admin/quota', () =>
    HttpResponse.json({
      keys: [
        { key_id: 1, label: 'K', node: 'n1', used: 10, limit: 100, state: 'ok', trend: 'steady' },
      ],
    }),
  ),
];
