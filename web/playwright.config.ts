import { defineConfig, devices } from '@playwright/test';

const port = Number(process.env.E2E_PORT ?? 4173);
const baseURL = process.env.E2E_BASE_URL ?? `http://127.0.0.1:${port}`;
const channel = process.env.PLAYWRIGHT_CHROME_CHANNEL;

export default defineConfig({
  testDir: './e2e',
  fullyParallel: true,
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 2 : 0,
  reporter: 'list',
  use: {
    ...devices['Desktop Chrome'],
    baseURL,
    colorScheme: 'light',
    trace: 'on-first-retry',
    ...(process.env.PLAYWRIGHT_CHROME_PATH === undefined
      ? {}
      : { launchOptions: { executablePath: process.env.PLAYWRIGHT_CHROME_PATH } }),
    ...(channel === undefined ? {} : { channel }),
  },
  webServer: {
    command: `npm run dev -- --host 127.0.0.1 --port ${port} --strictPort`,
    url: baseURL,
    reuseExistingServer: false,
    timeout: 120_000,
  },
});
