import { expect } from '@playwright/test';
import type { Page } from '@playwright/test';
import { expectNoSeriousViolations } from '../../e2e-mocked/support';

export async function expectIssuesPage(page: Page): Promise<void> {
  await expect(page.getByTestId('admin-issues-page')).toBeVisible();
}

export async function expectIncidentRow(page: Page, id: string, texts: string[]): Promise<void> {
  const row = page.getByTestId(`admin-issues-row-${id}`);
  for (const text of texts) await expect(row).toContainText(text);
}

export async function expectNoEventsLink(page: Page): Promise<void> {
  await expect(page.getByRole('link', { name: 'Events & audit' })).toHaveCount(0);
}

export async function expectTextAbsent(page: Page, text: string): Promise<void> {
  await expect(page.getByText(text)).toHaveCount(0);
}

export async function expectNoSeriousAccessibilityViolations(page: Page): Promise<void> {
  await expectNoSeriousViolations(page);
}

export async function expectPlanRow(page: Page, name: string, texts: string[]): Promise<void> {
  const row = page.getByTestId(`admin-defaults-plan-${name}`);
  for (const text of texts) await expect(row).toContainText(text);
}

export async function expectPlansTable(page: Page): Promise<void> {
  await expect(page.getByRole('table', { name: 'Plans' })).toBeVisible();
}

export async function clickRefreshPlans(page: Page): Promise<void> {
  await page.getByTestId('admin-defaults-refresh').click();
}

export async function expectAdminNavHidden(page: Page): Promise<void> {
  await expect(
    page.getByRole('navigation', { name: 'Admin sections', includeHidden: true }),
  ).toBeHidden();
}

export async function expectPlanRowAsCard(
  page: Page,
  name: string,
  contactsLabel: string,
): Promise<void> {
  const row = page.getByTestId(`admin-defaults-plan-${name}`);
  await expect(row).toBeVisible();
  expect(await row.evaluate((element) => getComputedStyle(element).display)).toBe('grid');
  const cell = row.locator('[data-label="Contacts"]');
  expect(await cell.evaluate((element) => getComputedStyle(element, '::before').content)).toBe(
    `"${contactsLabel}"`,
  );
}

export async function toggleAdminLanguage(page: Page): Promise<void> {
  await page.getByTestId('admin-lang-toggle').click();
}

export async function expectForbidden(page: Page): Promise<void> {
  await expect(page.getByTestId('shell-forbidden')).toBeVisible();
  await expect(page.getByTestId('admin-defaults-page')).toHaveCount(0);
}

export async function expectNodesHeading(page: Page, name: string): Promise<void> {
  await expect(page.getByRole('heading', { level: 1, name })).toBeVisible();
}

export async function expectKeyPlacement(
  page: Page,
  visible: string[],
  absent: string[],
): Promise<void> {
  const panel = page.getByTestId('admin-nodes-key-placement');
  for (const text of visible) await expect(panel.getByText(text, { exact: true })).toBeVisible();
  for (const text of absent) await expect(panel.getByText(text, { exact: true })).toHaveCount(0);
}

export async function clickRefreshNodes(page: Page): Promise<void> {
  await page.getByTestId('admin-nodes-refresh').click();
}

export async function pageWidths(page: Page): Promise<{ viewport: number; page: number }> {
  return page.evaluate(() => ({
    viewport: document.documentElement.clientWidth,
    page: document.documentElement.scrollWidth,
  }));
}
