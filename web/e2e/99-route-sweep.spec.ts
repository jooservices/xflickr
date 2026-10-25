import { expect, test } from '@playwright/test';
import type { Page } from '@playwright/test';
import { ADMIN_STATE, ensureAdminState, expectShell } from './support/auth';
import { expectLayout } from './support/layout';
import { expectNoErrorPage, monitor } from './support/monitor';
import { appRoutes } from './support/routes';
import type { AppRoute } from './support/routes';

// Visits every route of the app (read from the router, so a new route is swept automatically) at desktop and
// at 390 px, and fails on any exception, console error, failed or error response, HTML where JSON was
// expected, or error page. Protected routes are visited as a signed-in admin. `E2E_ANONYMOUS=1` (the deploy
// smoke without credentials) visits them as a visitor.
const anonymous = process.env.E2E_ANONYMOUS === '1';
const routes = appRoutes();
const visitor = { cookies: [], origins: [] };

async function visit(page: Page, route: AppRoute): Promise<void> {
  const watch = monitor(page);
  await page.goto(route.url);
  await page.waitForLoadState('networkidle');
  if (route.scope === 'public') {
    await expect(page).toHaveURL(new RegExp(`${route.url}$`));
    // Registration is closed once the first user exists, and the screen then says so instead of showing the form.
    await expect(
      page.getByTestId('auth-card').or(page.getByTestId('auth-register-closed')),
    ).toBeVisible();
  } else if (anonymous) {
    // A visitor sees the sign-in screen or the shell's signed-out state; either way nothing may break.
    await expect(
      page.getByTestId('auth-card').or(page.locator('[data-testid$="-topbar"]')),
    ).toBeVisible();
  } else {
    if (route.notFound) {
      await expect(page.getByTestId('shell-notfound')).toBeVisible();
    } else {
      await expect(page, `${route.url} stayed on its address`).toHaveURL(
        new RegExp(`${route.url.replace(/\/$/, '')}/?$`),
      );
      await expectShell(page);
    }
  }
  await expectNoErrorPage(page, route.url);
  if (anonymous && route.scope !== 'public') {
    // A visitor's data calls are answered 401 by design; anything else is still a failure.
    const rest = watch.problems.filter((problem) => !/^HTTP [A-Z]+ \S+ 401$/.test(problem));
    expect(rest, `problems on ${route.url}`).toEqual([]);
  } else {
    watch.expectClean(route.url);
  }
  watch.stop();
}

test('finds the routes of the router', () => {
  const urls = routes.map((route) => route.url);
  expect(urls).toEqual(
    expect.arrayContaining(['/', '/login', '/register', '/keys', '/crawl', '/admin']),
  );
  expect(routes.some((route) => route.notFound)).toBe(true);
});

for (const [name, viewport] of [
  ['desktop', { width: 1280, height: 800 }],
  ['mobile 390 px', { width: 390, height: 844 }],
] as const) {
  test.describe(`route sweep, ${name}`, () => {
    test.use({ viewport });

    test.describe('public screens', () => {
      test.use({ storageState: visitor });
      for (const route of routes.filter((r) => r.scope === 'public')) {
        test(route.url, async ({ page }) => {
          await visit(page, route);
        });
      }
    });

    test.describe(
      anonymous ? 'protected screens as a visitor' : 'protected screens as admin',
      () => {
        test.use({ storageState: anonymous ? visitor : ADMIN_STATE });
        test.beforeAll(async ({ browser, baseURL }) => {
          if (!anonymous) await ensureAdminState(browser, baseURL);
        });
        for (const route of routes.filter((r) => r.scope !== 'public')) {
          test(`${route.scope} ${route.url}`, async ({ page }) => {
            await visit(page, route);
          });
        }
      },
    );
  });
}

// Sign-in, keys and crawl in both themes at both widths: no sideways scroll, no overlapping controls.
if (!anonymous) {
  for (const theme of ['light', 'dark'] as const) {
    for (const [name, viewport] of [
      ['desktop', { width: 1280, height: 800 }],
      ['mobile 390 px', { width: 390, height: 844 }],
    ] as const) {
      test.describe(`layout, ${theme} theme, ${name}`, () => {
        test.use({ viewport, colorScheme: theme });

        test.beforeEach(async ({ page }) => {
          await page.addInitScript((value) => {
            window.localStorage.setItem('xf.theme.user', value);
          }, theme);
        });

        test.describe('/login', () => {
          test.use({ storageState: visitor });
          test('/login', async ({ page }) => {
            await page.goto('/login');
            await page.waitForLoadState('networkidle');
            await expect(page.getByTestId('auth-page')).toHaveAttribute('data-theme', theme);
            await expectLayout(page, `/login (${theme}, ${name})`);
          });
        });

        for (const url of ['/keys', '/crawl']) {
          test.describe(url, () => {
            test.use({ storageState: ADMIN_STATE });
            test(`${url}`, async ({ page }) => {
              await page.goto(url);
              await page.waitForLoadState('networkidle');
              await expect(page.locator(`[data-role="user"]`).first()).toHaveAttribute(
                'data-theme',
                theme,
              );
              await expectLayout(page, `${url} (${theme}, ${name})`);
            });
          });
        }
      });
    }
  }
}
