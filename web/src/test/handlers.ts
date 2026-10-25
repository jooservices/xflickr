import { http, HttpResponse } from 'msw/http';
import { crawlHandlers } from './crawl';

export const handlers = [
  ...crawlHandlers,
  http.get('http://localhost/api/health', () => HttpResponse.json({ status: 'ok' })),
  http.get('*/healthz', () => HttpResponse.json({ status: 'ok' })),
  http.get('*/auth/config', () =>
    HttpResponse.json({ registration_open: true, passkey_enabled: false }),
  ),
  http.get('*/me/sessions', () => HttpResponse.json({ items: [] })),
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
