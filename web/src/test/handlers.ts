import { http, HttpResponse } from 'msw/http';

export const handlers = [
  http.get('http://localhost/api/health', () => HttpResponse.json({ status: 'ok' })),
];
