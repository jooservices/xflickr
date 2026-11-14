import { expect } from '@playwright/test';
import type { Page } from '@playwright/test';

export async function expectKeysEmpty(page: Page): Promise<void> {
  await expect(page.getByTestId('keys-empty')).toBeVisible();
}

export async function clickAddFirstKey(page: Page): Promise<void> {
  await page.getByTestId('keys-empty-cta').click();
}

export async function fillKeyForm(
  page: Page,
  key: { label: string; key: string; secret: string },
): Promise<void> {
  await page.getByTestId('keys-add-label').fill(key.label);
  await page.getByTestId('keys-add-key').fill(key.key);
  await page.getByTestId('keys-add-secret').fill(key.secret);
}

export async function submitKeyForm(page: Page): Promise<void> {
  await page.getByTestId('keys-add-submit').click();
}

export async function expectKeyModalOpen(page: Page): Promise<void> {
  await expect(page.getByTestId('keys-add-modal')).toBeVisible();
}

export async function expectKeyFormError(page: Page): Promise<void> {
  await expect(
    page.getByTestId('keys-add-error').or(page.getByTestId('auth-error-key')),
  ).toBeVisible();
}

export async function expectKeyFormKept(page: Page, label: string, key: string): Promise<void> {
  await expect(page.getByTestId('keys-add-label')).toHaveValue(label);
  await expect(page.getByTestId('keys-add-key')).toHaveValue(key);
}

export async function expectKeysGrid(page: Page): Promise<void> {
  await expect(page.getByTestId('keys-grid')).toBeVisible();
}

export async function expectFirstKeyCard(page: Page, label: string): Promise<void> {
  await expect(page.getByTestId('keys-card-label').first()).toHaveText(label);
  await expect(page.getByTestId('keys-card-used').first()).toBeVisible();
  await expect(page.getByTestId('keys-card-bar').first()).toBeVisible();
  await expect(page.getByTestId('keys-card-state').first()).toBeVisible();
}

export async function clickConnectFlickrAccount(page: Page): Promise<void> {
  const connect = page.locator('[data-testid^="keys-accounts-connect-"]').first();
  await expect(connect).toBeVisible();
  // The app leaves for fakeflickr's authorize page, which redirects back to /flickr-accounts/callback.
  await connect.click();
}

export async function expectFlickrAccount(page: Page, nsid: string): Promise<void> {
  await expect(page.getByTestId(`keys-account-${nsid}`)).toBeVisible();
  await expect(page.getByTestId(`keys-account-status-${nsid}`)).toBeVisible();
}
