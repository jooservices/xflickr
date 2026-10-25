import { useEffect, useRef } from 'react';
import type { RefObject } from 'react';

const FOCUSABLE =
  'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])';

/**
 * Modal behaviour: Esc closes, Tab stays inside `container`, and focus returns to
 * the control that was focused before the dialog opened (when it still exists).
 * `focusFirst` runs once on open to place focus.
 */
export function useDialog(
  container: RefObject<HTMLElement | null>,
  onClose: () => void,
  focusFirst: () => void,
): void {
  const closeRef = useRef(onClose);
  const firstRef = useRef(focusFirst);

  useEffect(() => {
    closeRef.current = onClose;
    firstRef.current = focusFirst;
  }, [onClose, focusFirst]);

  useEffect(() => {
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
      if (opener !== document.body && opener?.isConnected === true) opener.focus();
    };
  }, [container]);
}
