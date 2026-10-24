import type { IconName, MessageKey } from '../../shared';

export type UserNavItem = { id: string; path: string; labelKey: MessageKey };

/** User shell tabs, in the order of docs/ui/views/user-shell.md. */
export const USER_NAV: readonly UserNavItem[] = [
  { id: 'home', path: '/', labelKey: 'shell.nav.home' },
  { id: 'library', path: '/library', labelKey: 'shell.nav.library' },
  { id: 'search', path: '/search', labelKey: 'shell.nav.search' },
  { id: 'contacts', path: '/contacts', labelKey: 'shell.nav.contacts' },
  { id: 'albums', path: '/albums', labelKey: 'shell.nav.albums' },
  { id: 'crawl', path: '/crawl', labelKey: 'shell.nav.crawl' },
  { id: 'keys', path: '/keys', labelKey: 'shell.nav.keys' },
  { id: 'downloads', path: '/downloads', labelKey: 'shell.nav.downloads' },
  { id: 'sync', path: '/sync', labelKey: 'shell.nav.sync' },
  { id: 'sharing', path: '/sharing', labelKey: 'shell.nav.sharing' },
  { id: 'profile', path: '/profile', labelKey: 'shell.nav.profile' },
];

export type AdminNavItem = { id: string; path: string; labelKey: MessageKey; icon: IconName };
export type AdminNavGroup = { id: string; labelKey: MessageKey; items: readonly AdminNavItem[] };

/** Admin rail groups and items, in the order of docs/ui/views/admin-shell.md. */
export const ADMIN_NAV_GROUPS: readonly AdminNavGroup[] = [
  {
    id: 'operations',
    labelKey: 'admin.nav.group.operations',
    items: [
      { id: 'overview', path: '/admin', labelKey: 'admin.nav.overview', icon: 'grid' },
      { id: 'nodes', path: '/admin/nodes', labelKey: 'admin.nav.nodes', icon: 'server' },
      { id: 'quota', path: '/admin/quota', labelKey: 'admin.nav.quota', icon: 'key' },
      { id: 'metrics', path: '/admin/metrics', labelKey: 'admin.nav.metrics', icon: 'chart' },
      { id: 'events', path: '/admin/events', labelKey: 'admin.nav.events', icon: 'list' },
      { id: 'issues', path: '/admin/issues', labelKey: 'admin.nav.issues', icon: 'flag' },
    ],
  },
  {
    id: 'pipelines',
    labelKey: 'admin.nav.group.pipelines',
    items: [
      {
        id: 'pipeline',
        path: '/admin/pipeline',
        labelKey: 'admin.nav.pipeline',
        icon: 'activity',
      },
      {
        id: 'downloads',
        path: '/admin/downloads',
        labelKey: 'admin.nav.downloads',
        icon: 'download',
      },
      { id: 'sync', path: '/admin/sync', labelKey: 'admin.nav.sync', icon: 'cloud' },
      { id: 'ai', path: '/admin/ai', labelKey: 'admin.nav.ai', icon: 'sparkle' },
    ],
  },
  {
    id: 'data',
    labelKey: 'admin.nav.group.data',
    items: [
      { id: 'users', path: '/admin/users', labelKey: 'admin.nav.users', icon: 'users' },
      { id: 'defaults', path: '/admin/defaults', labelKey: 'admin.nav.defaults', icon: 'sliders' },
      { id: 'sharing', path: '/admin/sharing', labelKey: 'admin.nav.sharing', icon: 'share' },
      { id: 'log', path: '/admin/log', labelKey: 'admin.nav.log', icon: 'eye' },
    ],
  },
];

export const ADMIN_NAV: readonly AdminNavItem[] = ADMIN_NAV_GROUPS.flatMap((group) => group.items);
