import type { Page } from '@playwright/test';
import { expect } from '@playwright/test';
import {
  expectProblemCountAtLeast,
  fetchEachInPage,
  fetchJsonInPage,
  fetchRefusedInPage,
  raiseConsoleErrorAndException,
  refuseConnectionAt,
  serveHtmlAt,
  serveStatusAt,
} from '../steps/seeded';
import { monitor } from '../steps/monitor';
import { openLogin } from '../steps/session';

/** Seeded failures: the monitor behind every journey and the route sweep must notice each kind of problem. */
export async function noticeHtmlInsteadOfJson(page: Page): Promise<void> {
  const watch = monitor(page);
  await serveHtmlAt(page, '/__e2e/html');
  await openLogin(page);
  await fetchJsonInPage(page, '/__e2e/html');
  expect(watch.problems.join('\n')).toContain('HTML instead of JSON: GET /__e2e/html 200');
  expect(() => watch.expectClean('seeded')).toThrow();
}

export async function noticeErrorStatusNotAllowListed(page: Page): Promise<void> {
  const watch = monitor(page);
  await serveStatusAt(page, '/__e2e/missing', 404);
  await serveStatusAt(page, '/__e2e/broken', 500);
  await openLogin(page);
  await fetchEachInPage(page, ['/__e2e/missing', '/__e2e/broken']);
  expect(watch.problems).toEqual(
    expect.arrayContaining(['HTTP GET /__e2e/missing 404', 'HTTP GET /__e2e/broken 500']),
  );
}

export async function noticeConsoleErrorAndException(page: Page): Promise<void> {
  const watch = monitor(page);
  await openLogin(page);
  await raiseConsoleErrorAndException(page);
  await expectProblemCountAtLeast(() => watch.problems.length, 2);
  expect(watch.problems.join('\n')).toContain('console.error: seeded console error');
  expect(watch.problems.join('\n')).toContain('uncaught exception: seeded exception');
}

export async function noticeFailedRequest(page: Page): Promise<void> {
  const watch = monitor(page);
  await refuseConnectionAt(page, '/__e2e/refused');
  await openLogin(page);
  await fetchRefusedInPage(page, '/__e2e/refused');
  await expect.poll(() => watch.problems.join('\n')).toContain('request failed: GET');
}

export async function tolerateOnlyProvokedResponses(page: Page): Promise<void> {
  const watch = monitor(page);
  await serveStatusAt(page, '/__e2e/provoked', 422);
  await openLogin(page);
  watch.expectFailure('GET', '/__e2e/provoked', 422);
  await fetchEachInPage(page, ['/__e2e/provoked']);
  watch.expectClean('provoked failure');
}
