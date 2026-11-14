import { test } from '@playwright/test';
import { ADMIN_STATE } from '../support/env';
import { suppressMissingPerson } from '../suites/catalog';

test.use({ storageState: ADMIN_STATE });

test('a missing person completes crawl work and stays suppressed after reopening', async ({
  page,
}) => {
  await suppressMissingPerson(page);
});
