import { useEffect, useRef } from 'react';
import { useLocation } from 'react-router';

/**
 * After a route change, scrolls to the top and moves focus to the page heading
 * (or `main` when the page has none). The first render is left alone so the
 * skip link stays the first Tab stop.
 */
export function useRouteFocus(): void {
  const { pathname } = useLocation();
  const previous = useRef(pathname);

  useEffect(() => {
    if (previous.current === pathname) return;
    previous.current = pathname;
    window.scrollTo?.(0, 0);
    const target =
      document.querySelector<HTMLElement>('#main h1') ?? document.getElementById('main');
    target?.focus({ preventScroll: true });
  }, [pathname]);
}
