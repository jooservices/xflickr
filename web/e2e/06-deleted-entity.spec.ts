import { expect, test } from '@playwright/test';
import { ADMIN_STATE } from './support/auth';
import { monitor } from './support/monitor';
import { worktreeSQL } from './support/worktree';

test.use({ storageState: ADMIN_STATE });

test('a missing person completes crawl work and stays suppressed after reopening', async ({
  page,
}) => {
  const watch = monitor(page);
  const missing = '99999999@N99';
  worktreeSQL(`INSERT INTO crawl_interests(user_id,person_nsid,depth,via)
    SELECT user_id,'${missing}',0,'e2e' FROM flickr_accounts LIMIT 1
    ON CONFLICT DO NOTHING;
    INSERT INTO work_items(kind,subject_key,user_id,key_id,depth,cursor)
    SELECT 'albums','${missing}',user_id,key_id,0,
      '{"page":1,"method":"flickr.photosets.getList","params":{"user_id":"${missing}","per_page":"500"}}'
    FROM flickr_accounts LIMIT 1 ON CONFLICT DO NOTHING;`);
  await expect
    .poll(
      () =>
        worktreeSQL(
          `SELECT count(*) FROM entity_states WHERE entity_type='person' AND entity_id='${missing}'`,
        ),
      { timeout: 30_000 },
    )
    .toBe('1');
  const first = worktreeSQL(
    `SELECT first_seen_at::text FROM entity_states WHERE entity_type='person' AND entity_id='${missing}'`,
  );
  worktreeSQL(`UPDATE work_items SET state='pending',not_before=now(),attempts=0
    WHERE kind='albums' AND subject_key='${missing}'`);
  await expect
    .poll(
      () =>
        worktreeSQL(
          `SELECT state FROM work_items WHERE kind='albums' AND subject_key='${missing}'`,
        ),
      { timeout: 30_000 },
    )
    .toBe('done');
  expect(
    worktreeSQL(
      `SELECT first_seen_at::text FROM entity_states WHERE entity_type='person' AND entity_id='${missing}'`,
    ),
  ).toBe(first);
  await page.goto('/crawl');
  await expect(page.getByTestId('user-crawl-page')).toBeVisible();
  watch.expectClean('/crawl with a deleted person');
});
