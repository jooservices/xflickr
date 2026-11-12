import type { ReactElement } from 'react';
import type { RouteObject } from 'react-router';
import { PlaceholderPage } from '../../pages';
import { AlbumsPage } from '../../pages/albums';
import { ContactsPage } from '../../pages/contacts/ContactsPage';
import { CrawlPage } from '../../pages/crawl';
import { HomePage } from '../../pages/home/HomePage';
import { KeysPage } from '../../pages/keys';
import { LibraryPage } from '../../pages/library';
import { ProfilePage } from '../../pages/profile/ProfilePage';
import { SetupPage } from '../../pages/setup/SetupPage';
import { USER_NAV } from '../shells/nav';
import type { UserNavItem } from '../shells/nav';
import { UserNotFound } from './UserNotFound';

/** Page of each user navigation path that has its own component. Other paths show a placeholder. */
const userPageTable: Record<string, () => ReactElement> = {
  '/library': () => <LibraryPage />,
  '/profile': () => <ProfilePage />,
  '/contacts': () => <ContactsPage />,
  '/crawl': () => <CrawlPage />,
  '/albums': () => <AlbumsPage />,
  '/keys': () => <KeysPage />,
};

function userElement(item: UserNavItem): ReactElement {
  const page: (() => ReactElement) | undefined = userPageTable[item.path];
  return page ? page() : <PlaceholderPage scope="shell" titleKey={item.labelKey} unavailable />;
}

/** Child routes of the user shell. Add a page by adding its path to `userPageTable`. */
export const userPages: RouteObject[] = [
  { index: true, element: <HomePage /> },
  ...USER_NAV.filter((item) => item.path !== '/').map((item): RouteObject => ({
    path: item.path.slice(1),
    element: userElement(item),
  })),
  {
    path: 'contacts/:id',
    element: <PlaceholderPage scope="shell" titleKey="shell.page.contact" unavailable />,
  },
  {
    path: 'albums/:id',
    element: <PlaceholderPage scope="shell" titleKey="shell.page.album" unavailable />,
  },
  {
    path: 'accounts',
    element: <PlaceholderPage scope="shell" titleKey="shell.page.accounts" unavailable />,
  },
  {
    path: 'sessions',
    element: <PlaceholderPage scope="shell" titleKey="shell.page.sessions" unavailable />,
  },
  { path: 'onboarding', element: <SetupPage /> },
  { path: '*', element: <UserNotFound /> },
];
