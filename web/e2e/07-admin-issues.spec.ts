import { expect, test } from '@playwright/test';
import { ADMIN_STATE, apiRead } from './support/auth';
import { monitor } from './support/monitor';
import { worktreeSQL } from './support/worktree';

test.use({ storageState: ADMIN_STATE });

test('an admin sees only safe fields for an open incident', async ({ page }) => {
  const marker = Date.now().toString();
  const kind = `e2e.issues.${marker}`;
  const scope = `e2e:${marker}`;
  const unsafeMarker = `private-${marker}`;
  const id = worktreeSQL(`
    INSERT INTO incidents (kind, scope, severity, title, detail, first_seen, last_seen, count)
    VALUES ('${kind}', '${scope}', 'critical', '${unsafeMarker}',
      '{"evidence":"${unsafeMarker}"}'::jsonb, now(), now() + interval '5 minutes', 3)
    RETURNING id;
  `);
  const watch = monitor(page);

  try {
    await page.goto('/admin/issues');
    await expect(page.getByTestId('admin-issues-page')).toBeVisible();
    const row = page.getByTestId(`admin-issues-row-${id}`);
    await expect(row).toContainText(`Incident #${id}`);
    await expect(row).toContainText('Critical');
    await expect(row).toContainText('3');
    await expect(page.getByRole('link', { name: 'Events & audit' })).toHaveCount(0);
    await expect(page.getByText(unsafeMarker)).toHaveCount(0);

    const response = await apiRead(page, '/admin/incidents?limit=100');
    expect(response.status).toBe(200);
    const body = response.body as {
      items: Record<string, unknown>[];
      next_cursor: string | null;
    };
    const item = body.items.find((candidate) => candidate.id === Number(id));
    expect(item).toMatchObject({ id: Number(id), severity: 'critical', count: 3 });
    expect(Object.keys(item ?? {}).sort()).toEqual(
      ['count', 'first_seen', 'id', 'last_seen', 'severity'].sort(),
    );
    expect(JSON.stringify(response.body)).not.toContain(unsafeMarker);
    watch.expectClean('/admin/issues');
  } finally {
    watch.stop();
    worktreeSQL(`DELETE FROM incidents WHERE kind = '${kind}' AND scope = '${scope}';`);
  }
});
