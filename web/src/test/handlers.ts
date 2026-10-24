import { http, HttpResponse } from 'msw/http';

export const adminShellFixture = {
  open_issues: 3,
  unread_notifications: 2,
  cluster: {
    status: 'ok',
    nodes_total: 4,
    nodes_healthy: 4,
    calls_per_second: 12,
    calls_history: Array.from({ length: 60 }, (_, index) => index % 7),
  },
  generated_at: '2026-10-25T10:00:00Z',
};

export const handlers = [
  http.get('http://localhost/api/health', () => HttpResponse.json({ status: 'ok' })),
  http.get('*/healthz', () => HttpResponse.json({ status: 'ok' })),
  http.get('*/shell/summary', () =>
    HttpResponse.json({
      unread_notifications: 3,
      running_downloads: 2,
      tray_count: 12,
      attention: [],
    }),
  ),
  http.get('*/tray', () => HttpResponse.json({ items: [], count: 0, estimated_bytes: 0 })),
  http.get('*/notifications', () => HttpResponse.json({ items: [] })),
  http.get('*/admin/shell', () => HttpResponse.json(adminShellFixture)),
];
