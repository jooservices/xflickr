import { useEffect, useRef } from 'react';
import type { RefObject } from 'react';

const FOCUSABLE =
  'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])';

/** Keeps modal focus inside its container and returns it to the opener on close. */
export function useDialog(
  container: RefObject<HTMLElement | null>,
  onClose: () => void,
  focusFirst: () => void,
  enabled = true,
  returnFocus?: () => HTMLElement | null,
): void {
  const closeRef = useRef(onClose);
  const firstRef = useRef(focusFirst);
  const returnRef = useRef(returnFocus);

  useEffect(() => {
    closeRef.current = onClose;
    firstRef.current = focusFirst;
    returnRef.current = returnFocus;
  }, [onClose, focusFirst, returnFocus]);

  useEffect(() => {
    if (!enabled) return;
    const opener = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    firstRef.current();
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.stopPropagation();
        closeRef.current();
        return;
      }
      const root = container.current;
      if (event.key !== 'Tab' || root === null) return;
      const items = Array.from(root.querySelectorAll<HTMLElement>(FOCUSABLE));
      const head = items[0];
      const tail = items[items.length - 1];
      if (head === undefined || tail === undefined) return;
      if (event.shiftKey && document.activeElement === head) {
        event.preventDefault();
        tail.focus();
      } else if (!event.shiftKey && document.activeElement === tail) {
        event.preventDefault();
        head.focus();
      }
    };
    document.addEventListener('keydown', onKeyDown);
    return () => {
      document.removeEventListener('keydown', onKeyDown);
      const target = returnRef.current?.() ?? opener;
      if (target !== document.body && target?.isConnected === true) target.focus();
    };
  }, [container, enabled]);
}
