import { defineConfig, devices } from '@playwright/test';

// The real stack: `make e2e` (docker/scripts/e2e.sh) starts the backend, fakeflickr and the built web bundle and sets
// E2E_BASE_URL. Nothing here starts a server or mocks a route.
const baseURL = process.env.E2E_BASE_URL;
const channel = process.env.PLAYWRIGHT_CHROME_CHANNEL;

export default defineConfig({
  testDir: './e2e',
  // The journeys build on each other's state (first user, keys, accounts), so the suite runs in file order.
  fullyParallel: false,
  workers: 1,
  forbidOnly: Boolean(process.env.CI),
  retries: 0,
  reporter: 'list',
  timeout: 60_000,
  use: {
    ...devices['Desktop Chrome'],
    ...(baseURL === undefined ? {} : { baseURL }),
    colorScheme: 'light',
    trace: 'retain-on-failure',
    ...(channel === undefined ? {} : { channel }),
  },
});
