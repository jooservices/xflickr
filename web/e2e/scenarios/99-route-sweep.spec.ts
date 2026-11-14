import { test } from '@playwright/test';
import { ADMIN_STATE, ANONYMOUS, VIEWPORTS, VISITOR_STATE } from '../support/env';
import { prepareAdminSession } from '../suites/auth';
import {
  checkMemberScreenLayout,
  checkSignInLayout,
  chooseTheme,
  findRoutesOfTheRouter,
  routes,
  visitRoute,
} from '../suites/sweep';

// Visits every route of the app (read from the router, so a new route is swept automatically) at desktop and
// at 390 px. Protected routes are visited as a signed-in admin; `E2E_ANONYMOUS=1` (the deploy smoke without
// credentials) visits them as a visitor.
test('finds the routes of the router', () => {
  findRoutesOfTheRouter();
});

for (const [name, viewport] of [
  ['desktop', VIEWPORTS.desktop],
  ['mobile 390 px', VIEWPORTS.mobile],
] as const) {
  test.describe(`route sweep, ${name}`, () => {
    test.use({ viewport });

    test.describe('public screens', () => {
      test.use({ storageState: VISITOR_STATE });
      for (const route of routes.filter((r) => r.scope === 'public')) {
        test(route.url, async ({ page }) => {
          await visitRoute(page, route);
        });
      }
    });

    test.describe(
      ANONYMOUS ? 'protected screens as a visitor' : 'protected screens as admin',
      () => {
        test.use({ storageState: ANONYMOUS ? VISITOR_STATE : ADMIN_STATE });
        test.beforeAll(async ({ browser, baseURL }) => {
          if (!ANONYMOUS) await prepareAdminSession(browser, baseURL);
        });
        for (const route of routes.filter((r) => r.scope !== 'public')) {
          test(`${route.scope} ${route.url}`, async ({ page }) => {
            await visitRoute(page, route);
          });
        }
      },
    );
  });
}

// Sign-in, keys and crawl in both themes at both widths: no sideways scroll, no overlapping controls.
if (!ANONYMOUS) {
  for (const theme of ['light', 'dark'] as const) {
    for (const [name, viewport] of [
      ['desktop', VIEWPORTS.desktop],
      ['mobile 390 px', VIEWPORTS.mobile],
    ] as const) {
      test.describe(`layout, ${theme} theme, ${name}`, () => {
        test.use({ viewport, colorScheme: theme });

        test.beforeEach(async ({ page }) => {
          await chooseTheme(page, theme);
        });

        test.describe('/login', () => {
          test.use({ storageState: VISITOR_STATE });
          test('/login', async ({ page }) => {
            await checkSignInLayout(page, theme, name);
          });
        });

        for (const url of ['/keys', '/crawl']) {
          test.describe(url, () => {
            test.use({ storageState: ADMIN_STATE });
            test(`${url}`, async ({ page }) => {
              await checkMemberScreenLayout(page, url, theme, name);
            });
          });
        }
      });
    }
  }
}
