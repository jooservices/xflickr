import { defineConfig, devices } from '@playwright/test';

// The earlier browser specs: every API route is mocked, so they check UI states only and never replace the
// real-stack suite in e2e/ (playwright.config.ts).
const port = Number(process.env.E2E_PORT ?? 4173);
const baseURL = `http://127.0.0.1:${port}`;
const channel = process.env.PLAYWRIGHT_CHROME_CHANNEL;

export default defineConfig({
  testDir: './e2e-mocked',
  fullyParallel: true,
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 2 : 0,
  reporter: 'list',
  use: {
    ...devices['Desktop Chrome'],
    baseURL,
    colorScheme: 'light',
    trace: 'on-first-retry',
    ...(channel === undefined ? {} : { channel }),
  },
  webServer: {
    command: `npm run dev -- --host 127.0.0.1 --port ${port} --strictPort`,
    url: baseURL,
    reuseExistingServer: false,
    timeout: 120_000,
  },
});
