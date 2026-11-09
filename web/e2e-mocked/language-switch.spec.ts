import { expect } from '@playwright/test';
import { mockApi, test } from './support';

/** English UI labels that must disappear after switching to Vietnamese. */
const englishLabels = [
  'Sign in',
  'Create your account',
  'Keys & quota',
  'Crawl',
  'Profile',
  'Library',
  'Sections',
  'Set up',
];

async function mockAuthConfig(page: import('@playwright/test').Page) {
  await page.route('**/auth/config', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        registration_open: true,
        passkey_enabled: false,
        password_min: 8,
        password_max: 128,
      }),
    }),
  );
}

test.describe('language switch', () => {
  test('switches to Vietnamese across auth and shell pages, then back to English', async ({
    page,
  }) => {
    await mockApi(page, 'anonymous');
    await mockAuthConfig(page);

    await page.goto('/login');
    await expect(page.getByTestId('auth-login-title')).toHaveText('Sign in');
    await page.getByTestId('auth-lang-toggle').click();
    await expect(page.getByTestId('auth-login-title')).toHaveText('Đăng nhập');
    for (const label of ['Sign in', 'Create your account']) {
      await expect(page.getByText(label, { exact: true })).toHaveCount(0);
    }

    await page.goto('/register');
    await expect(page.getByTestId('auth-register-title')).toHaveText('Tạo tài khoản của bạn');
    await expect(page.getByText('Create your account', { exact: true })).toHaveCount(0);

    await mockApi(page, 'user');
    await page.goto('/onboarding');
    await expect(page.getByTestId('wizard-title')).toHaveText('Dùng khóa API Flickr của riêng bạn');
    await expect(page.getByText('Set up', { exact: true })).toHaveCount(0);

    await page.goto('/keys');
    await expect(page.getByTestId('keys-title')).toHaveText('Khóa & hạn mức');
    await expect(page.getByText('Keys & quota', { exact: true })).toHaveCount(0);

    await page.goto('/crawl');
    await expect(page.getByTestId('user-crawl-title')).toHaveText('Thu thập');
    await expect(page.getByText('Crawl', { exact: true })).toHaveCount(0);

    await page.goto('/profile');
    await expect(page.getByTestId('profile-title')).toHaveText('Hồ sơ');
    await expect(page.getByText('Profile', { exact: true })).toHaveCount(0);

    for (const label of englishLabels) {
      await expect(page.getByText(label, { exact: true })).toHaveCount(0);
    }

    await page.getByTestId('shell-lang-toggle').click();
    await expect(page.getByTestId('profile-title')).toHaveText('Profile');
  });
});
