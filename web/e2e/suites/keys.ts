import type { APIRequestContext, Page } from '@playwright/test';
import { expect } from '@playwright/test';
import { SEEDED_NSID } from '../support/env';
import { setFlickrScenario } from '../steps/api';
import { expectNoErrorPage, monitor } from '../steps/monitor';
import {
  clickAddFirstKey,
  clickConnectFlickrAccount,
  expectFirstKeyCard,
  expectFlickrAccount,
  expectKeyFormError,
  expectKeyFormKept,
  expectKeyModalOpen,
  expectKeysEmpty,
  expectKeysGrid,
  fillKeyForm,
  submitKeyForm,
} from '../steps/keys';
import { openPage, waitForUrl } from '../steps/session';

const VALID_KEY = '0123456789abcdef0123456789abcdef';
const VALID_SECRET = '0123456789abcdef';
export const DEAD_KEY = 'deadbeefdeadbeefdeadbeefdeadbeef';

/** Makes fakeflickr reject the dead key. */
export async function rejectDeadKeys(request: APIRequestContext): Promise<void> {
  await setFlickrScenario(request, { invalid_keys: [DEAD_KEY] });
}

export async function showEmptyKeys(page: Page): Promise<void> {
  const watch = monitor(page);
  await openPage(page, '/keys');
  await expectKeysEmpty(page);
  await expectNoErrorPage(page, '/keys');
  watch.expectClean('/keys (empty)');
}

export async function rejectInvalidKey(page: Page): Promise<void> {
  const watch = monitor(page);
  await openPage(page, '/keys');
  await clickAddFirstKey(page);
  await fillKeyForm(page, { label: 'Dead key', key: DEAD_KEY, secret: VALID_SECRET });
  watch.expectFailure('POST', '/keys', 422);
  watch.expectFailure('POST', '/keys', 400);
  await submitKeyForm(page);
  await expectKeyModalOpen(page);
  await expectKeyFormError(page);
  await expectKeyFormKept(page, 'Dead key', DEAD_KEY);
  await expectNoErrorPage(page, 'invalid key');
  expect(
    watch.problems.filter((problem) => !/HTTP POST \/keys /.test(problem)),
    'unexpected problems while adding an invalid key',
  ).toEqual([]);
}

export async function addValidKey(page: Page): Promise<void> {
  const watch = monitor(page);
  await openPage(page, '/keys');
  await clickAddFirstKey(page);
  await fillKeyForm(page, { label: 'E2E key', key: VALID_KEY, secret: VALID_SECRET });
  await submitKeyForm(page);
  await expectKeysGrid(page);
  await expectFirstKeyCard(page, 'E2E key');
  await expectNoErrorPage(page, '/keys with a key');
  watch.expectClean('/keys with a key');
}

/** Authorizes through fakeflickr OAuth, comes back through the callback and shows the account. */
export async function connectFlickrAccount(page: Page, request: APIRequestContext): Promise<void> {
  await setFlickrScenario(request, {});
  const watch = monitor(page);
  await openPage(page, '/keys');
  await expectKeysGrid(page);
  await clickConnectFlickrAccount(page);
  await waitForUrl(page, /\/keys/);
  await expectFlickrAccount(page, SEEDED_NSID);
  await expectNoErrorPage(page, '/keys after connecting');
  watch.expectClean('Flickr OAuth round trip');
}
