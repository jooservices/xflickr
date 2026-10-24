import { useEffect, useRef, useState } from 'react';

export const SKELETON_DELAY_MS = 200;
export const SKELETON_MIN_MS = 400;

/**
 * Decides when a skeleton is visible: only after `delay` ms of loading, so fast
 * answers never flash, and then for at least `min` ms so it never flickers.
 */
export function useLoadingTiming(
  loading: boolean,
  delay = SKELETON_DELAY_MS,
  min = SKELETON_MIN_MS,
): boolean {
  const [visible, setVisible] = useState(false);
  const shownAt = useRef(0);

  useEffect(() => {
    if (loading) {
      const timer = window.setTimeout(() => {
        shownAt.current = Date.now();
        setVisible(true);
      }, delay);
      return () => {
        window.clearTimeout(timer);
      };
    }
    if (!visible) return undefined;
    const remaining = Math.max(0, min - (Date.now() - shownAt.current));
    const timer = window.setTimeout(() => {
      setVisible(false);
    }, remaining);
    return () => {
      window.clearTimeout(timer);
    };
  }, [loading, visible, delay, min]);

  return visible;
}
