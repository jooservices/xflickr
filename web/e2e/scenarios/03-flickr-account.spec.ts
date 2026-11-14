import { test } from '@playwright/test';
import { ADMIN_STATE } from '../support/env';
import { connectFlickrAccount } from '../suites/keys';

test.use({ storageState: ADMIN_STATE });

test.describe.serial('connecting a Flickr account through fakeflickr OAuth', () => {
  test('authorizes, comes back through the callback and shows the account', async ({
    page,
    request,
  }) => {
    await connectFlickrAccount(page, request);
  });
});
