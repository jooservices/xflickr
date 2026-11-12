import type { ReactElement } from 'react';
import type { RouteObject } from 'react-router';
import { PlaceholderPage } from '../../pages';
import { IssuesPage } from '../../pages/admin/issues';
import { DefaultsPage } from '../../pages/admin/defaults/DefaultsPage';
import { NodesPage } from '../../pages/admin/nodes/NodesPage';
import { OverviewPage } from '../../pages/admin/overview/OverviewPage';
import { QuotaPage } from '../../pages/admin/quota';
import { ADMIN_NAV } from '../shells/nav';
import type { AdminNavItem } from '../shells/nav';
import { UserNotFound } from './UserNotFound';

/** Page of each admin navigation id that has its own component. Other ids show a placeholder. */
const adminPageTable: Record<string, () => ReactElement> = {
  overview: () => <OverviewPage />,
  defaults: () => <DefaultsPage />,
  nodes: () => <NodesPage />,
  quota: () => <QuotaPage />,
  issues: () => <IssuesPage />,
};

function adminElement(item: AdminNavItem): ReactElement {
  const page: (() => ReactElement) | undefined = adminPageTable[item.id];
  return page ? page() : <PlaceholderPage scope="admin" titleKey={item.labelKey} />;
}

/** Child routes of the admin shell. Add a page by adding its id to `adminPageTable`. */
export const adminPages: RouteObject[] = [
  { index: true, element: adminPageTable.overview() },
  ...ADMIN_NAV.filter((item) => item.path !== '/admin').map((item): RouteObject => ({
    path: item.path.slice('/admin/'.length),
    element: adminElement(item),
  })),
  { path: 'nodes/:id', element: <PlaceholderPage scope="admin" titleKey="admin.nav.nodes" /> },
  { path: '*', element: <UserNotFound /> },
];
