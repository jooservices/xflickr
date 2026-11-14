import { test } from '@playwright/test';
import {
  noticeConsoleErrorAndException,
  noticeErrorStatusNotAllowListed,
  noticeFailedRequest,
  noticeHtmlInsteadOfJson,
  tolerateOnlyProvokedResponses,
} from '../suites/monitor';

// Seeded failures: the monitor behind every journey and the route sweep must notice each kind of problem.
// (The same detection caught the real bug this suite was written for: GET /keys answered with the page.)
test.describe('the monitor fails on', () => {
  test('an endpoint that answers HTML instead of JSON', async ({ page }) => {
    await noticeHtmlInsteadOfJson(page);
  });

  test('an error status that is not allow-listed', async ({ page }) => {
    await noticeErrorStatusNotAllowListed(page);
  });

  test('a console error and an uncaught exception', async ({ page }) => {
    await noticeConsoleErrorAndException(page);
  });

  test('a failed request', async ({ page }) => {
    await noticeFailedRequest(page);
  });

  test('tolerates only the responses a journey provokes', async ({ page }) => {
    await tolerateOnlyProvokedResponses(page);
  });
});
