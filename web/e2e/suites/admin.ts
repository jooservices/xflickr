import type { Page } from '@playwright/test';
import { expect } from '@playwright/test';
import { adminCredentials } from '../support/env';
import {
  deleteIncident,
  deletePlan,
  firstKey,
  insertIncident,
  insertPlan,
  relabelKey,
  setPlanContacts,
  setUserRole,
} from '../support/factories';
import type { PlanSeed } from '../support/factories';
import { apiRead } from '../steps/api';
import {
  clickRefreshNodes,
  clickRefreshPlans,
  expectAdminNavHidden,
  expectForbidden,
  expectIncidentRow,
  expectIssuesPage,
  expectKeyPlacement,
  expectNoEventsLink,
  expectNoSeriousAccessibilityViolations,
  expectNodesHeading,
  expectPlanRow,
  expectPlanRowAsCard,
  expectPlansTable,
  expectTextAbsent,
  pageWidths,
  toggleAdminLanguage,
} from '../steps/admin';
import { expectLayout } from '../steps/layout';
import { expectNoErrorPage, monitor } from '../steps/monitor';
import { openPage, setViewport } from '../steps/session';
import { VIEWPORTS } from '../support/env';

/** An admin sees only the safe fields of an open incident, on the screen and in the API. */
export async function showSafeIncidentFields(page: Page): Promise<void> {
  const incident = insertIncident(Date.now().toString());
  const watch = monitor(page);
  try {
    await openPage(page, '/admin/issues');
    await expectIssuesPage(page);
    await expectIncidentRow(page, incident.id, [`Incident #${incident.id}`, 'Critical', '3']);
    await expectNoEventsLink(page);
    await expectTextAbsent(page, incident.unsafeMarker);
    const response = await apiRead(page, '/admin/incidents?limit=100');
    expect(response.status).toBe(200);
    const body = response.body as { items: Record<string, unknown>[]; next_cursor: string | null };
    const item = body.items.find((candidate) => candidate.id === Number(incident.id));
    expect(item).toMatchObject({ id: Number(incident.id), severity: 'critical', count: 3 });
    expect(Object.keys(item ?? {}).sort()).toEqual(
      ['count', 'first_seen', 'id', 'last_seen', 'severity'].sort(),
    );
    expect(JSON.stringify(response.body)).not.toContain(incident.unsafeMarker);
    watch.expectClean('/admin/issues');
  } finally {
    watch.stop();
    deleteIncident(incident);
  }
}

const STUDIO: PlanSeed = {
  name: 'Studio',
  weight: 3,
  maxDepthCap: 4,
  maxContacts: 1200,
  aiQuotaPerDay: 2500,
  zipTtlHours: 48,
  resourceShare: 0.375,
};

/** An admin reviews a seeded plan, refreshes it after it changed, and sees a usable layout on a phone and in Vietnamese. */
export async function reviewAndRefreshPlans(page: Page): Promise<void> {
  insertPlan(STUDIO);
  const watch = monitor(page);
  try {
    await openPage(page, '/admin/defaults');
    await expectPlanRow(page, STUDIO.name, ['1,200', '37.5%']);
    await expectPlansTable(page);
    setPlanContacts(STUDIO.name, 2400);
    await clickRefreshPlans(page);
    await expectPlanRow(page, STUDIO.name, ['2,400']);
    await expectNoSeriousAccessibilityViolations(page);
    await setViewport(page, VIEWPORTS.mobile);
    await expectAdminNavHidden(page);
    await expectLayout(page, 'admin defaults mobile');
    await expectPlanRowAsCard(page, STUDIO.name, 'Contacts');
    await expectNoSeriousAccessibilityViolations(page);
    await toggleAdminLanguage(page);
    await expectPlanRow(page, STUDIO.name, ['2.400']);
    await expectLayout(page, 'admin defaults mobile Vietnamese');
    watch.expectClean('admin defaults');
  } finally {
    watch.stop();
    deletePlan(STUDIO.name);
  }
}

/** A member who is not an admin cannot enter the admin defaults route (the admin is a plain member for the visit). */
export async function keepMemberOutOfAdminDefaults(page: Page): Promise<void> {
  const { email } = adminCredentials();
  setUserRole(email, 'user');
  try {
    await openPage(page, '/admin/defaults');
    await expectForbidden(page);
  } finally {
    setUserRole(email, 'admin');
  }
}

/** An admin reads the key states of the cluster, refreshes them, switches language and sees no sideways scroll on a phone. */
export async function readAndRefreshKeyStates(page: Page): Promise<void> {
  const key = firstKey();
  relabelKey(key.id, 'First key label');
  const watch = monitor(page);
  try {
    await openPage(page, '/admin/nodes');
    await expectNodesHeading(page, 'Cluster nodes');
    // The node that reported the key is a reporting detail the placement panel never shows.
    const quota = await apiRead(page, '/admin/quota');
    const reported = (quota.body as { keys: Array<{ key_id: number; node?: string }> }).keys.find(
      (item) => item.key_id === key.id,
    );
    await expectKeyPlacement(
      page,
      ['First key label', `#${key.id}`, `User #${key.userId}`],
      reported?.node ? [reported.node] : [],
    );
    relabelKey(key.id, 'Updated key label');
    await clickRefreshNodes(page);
    await expectKeyPlacement(page, ['Updated key label'], []);
    await toggleAdminLanguage(page);
    await expectNodesHeading(page, 'Các node');
    await expectKeyPlacement(page, [`Người dùng #${key.userId}`], []);
    await setViewport(page, VIEWPORTS.mobile);
    const widths = await pageWidths(page);
    expect(widths.page).toBeLessThanOrEqual(widths.viewport);
    await expectNoErrorPage(page, '/admin/nodes');
    watch.expectClean('/admin/nodes');
  } finally {
    watch.stop();
    relabelKey(key.id, key.label);
  }
}
