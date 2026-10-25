import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

export type AppRoute = {
  /** The concrete URL to visit; `:param` segments are filled with a sample value. */
  url: string;
  scope: 'public' | 'user' | 'admin';
  /** The catch-all route: the sweep expects the app's not-found screen here. */
  notFound?: boolean;
};

const read = (relative: string) =>
  readFileSync(fileURLToPath(new URL(`../../src/app/${relative}`, import.meta.url)), 'utf8');

const pathsIn = (source: string) => [...source.matchAll(/\bpath: '([^']+)'/g)].map((m) => m[1]);
const fill = (path: string) => path.replace(/:[A-Za-z]+/g, '1');

/**
 * Every route of the app, read from the router (`router.tsx`) and the navigation tables it builds routes from
 * (`shells/nav.ts`), so a new route is swept without touching the tests. Node cannot import the router (it pulls in
 * CSS and Vite's `import.meta.env`), so the sources are read as text.
 */
export function appRoutes(): AppRoute[] {
  const router = read('router.tsx');
  const adminStart = router.indexOf('const adminPages');
  const rootStart = router.indexOf('export const routes');
  if (adminStart < 0 || rootStart < 0)
    throw new Error('router.tsx no longer has adminPages and routes');

  const found = new Map<string, AppRoute>();
  const add = (route: AppRoute) => found.set(route.url, route);
  const place = (path: string, scope: 'user' | 'admin'): void => {
    if (path === '*') {
      add({
        url: scope === 'admin' ? '/admin/__e2e-unknown' : '/__e2e-unknown',
        scope,
        notFound: true,
      });
    } else {
      add({ url: scope === 'admin' ? `/admin/${fill(path)}` : `/${fill(path)}`, scope });
    }
  };

  for (const path of pathsIn(read('shells/nav.ts'))) {
    add({ url: fill(path), scope: path.startsWith('/admin') ? 'admin' : 'user' });
  }
  for (const path of pathsIn(router.slice(0, adminStart))) place(path, 'user');
  for (const path of pathsIn(router.slice(adminStart, rootStart))) place(path, 'admin');
  for (const path of pathsIn(router.slice(rootStart))) {
    if (path === '/admin') continue;
    add({ url: path, scope: path === '/login' || path === '/register' ? 'public' : 'user' });
  }
  add({ url: '/', scope: 'user' });
  return [...found.values()];
}
