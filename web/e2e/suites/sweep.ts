import type { Page } from '@playwright/test';
import { expect } from '@playwright/test';
import { ANONYMOUS } from '../support/env';
import { appRoutes } from '../support/routes';
import type { AppRoute } from '../support/routes';
import { expectLayout } from '../steps/layout';
import { expectNoErrorPage, monitor } from '../steps/monitor';
import { expectShell, openPage, waitForIdleNetwork } from '../steps/session';
import {
  expectAuthTheme,
  expectNotFoundScreen,
  expectPublicScreen,
  expectStayedOnAddress,
  expectUserTheme,
  expectVisitorScreen,
  applyTheme,
} from '../steps/sweep';

export const routes = appRoutes();

/** The router is read as text, so a new route is swept without touching the tests. */
export function findRoutesOfTheRouter(): void {
  const urls = routes.map((route) => route.url);
  expect(urls).toEqual(
    expect.arrayContaining(['/', '/login', '/register', '/keys', '/crawl', '/admin']),
  );
  expect(routes.some((route) => route.notFound)).toBe(true);
}

/** Visits one route and fails on any exception, console error, failed or error response, HTML where JSON was expected, or error page. */
export async function visitRoute(page: Page, route: AppRoute): Promise<void> {
  const watch = monitor(page);
  await openPage(page, route.url);
  await waitForIdleNetwork(page);
  if (route.scope === 'public') {
    await expectPublicScreen(page, route.url);
  } else if (ANONYMOUS) {
    await expectVisitorScreen(page);
  } else if (route.notFound) {
    await expectNotFoundScreen(page);
  } else {
    await expectStayedOnAddress(page, route.url);
    await expectShell(page);
  }
  await expectNoErrorPage(page, route.url);
  if (ANONYMOUS && route.scope !== 'public') {
    // A visitor's data calls are answered 401 by design; anything else is still a failure.
    const rest = watch.problems.filter((problem) => !/^HTTP [A-Z]+ \S+ 401$/.test(problem));
    expect(rest, `problems on ${route.url}`).toEqual([]);
  } else {
    watch.expectClean(route.url);
  }
  watch.stop();
}

export async function chooseTheme(page: Page, theme: 'light' | 'dark'): Promise<void> {
  await applyTheme(page, theme);
}

/** The sign-in screen in one theme at one width: the theme applies, nothing scrolls sideways, no controls overlap. */
export async function checkSignInLayout(page: Page, theme: string, size: string): Promise<void> {
  await openPage(page, '/login');
  await waitForIdleNetwork(page);
  await expectAuthTheme(page, theme);
  await expectLayout(page, `/login (${theme}, ${size})`);
}

/** A member screen in one theme at one width. */
export async function checkMemberScreenLayout(
  page: Page,
  url: string,
  theme: string,
  size: string,
): Promise<void> {
  await openPage(page, url);
  await waitForIdleNetwork(page);
  await expectUserTheme(page, theme);
  await expectLayout(page, `${url} (${theme}, ${size})`);
}
