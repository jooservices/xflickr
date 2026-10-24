import { useEffect, useRef } from 'react';
import type { RefObject } from 'react';

const FOCUSABLE =
  'a[href], button:not([disabled]), input:not([disabled]), [tabindex]:not([tabindex="-1"])';

type OverlayOptions = {
  open: boolean;
  onClose: () => void;
  /** Trap Tab inside the overlay (drawers, sheets and dialogs). */
  trap?: boolean;
  /** Close when a pointer goes down outside the overlay and its opener (popovers). */
  closeOnOutside?: boolean;
  /** The control that opened the overlay; focus returns to it. Falls back to the focused element. */
  openerRef?: RefObject<HTMLElement | null>;
};

/**
 * Focus management shared by menus, popovers, drawers and dialogs: focus moves
 * in on open, Esc closes, and focus returns to the opener on close.
 */
export function useOverlay<T extends HTMLElement>({
  open,
  onClose,
  trap = false,
  closeOnOutside = false,
  openerRef,
}: OverlayOptions): RefObject<T | null> {
  const ref = useRef<T | null>(null);
  const onCloseRef = useRef(onClose);

  useEffect(() => {
    onCloseRef.current = onClose;
  }, [onClose]);

  useEffect(() => {
    if (!open) return undefined;
    const opener =
      openerRef?.current ??
      (document.activeElement instanceof HTMLElement ? document.activeElement : null);
    const container = ref.current;
    const first = container?.querySelector<HTMLElement>(FOCUSABLE);
    (first ?? container)?.focus();

    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.stopPropagation();
        onCloseRef.current();
        return;
      }
      if (!trap || event.key !== 'Tab' || container === null) return;
      const items = Array.from(container.querySelectorAll<HTMLElement>(FOCUSABLE));
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
    const onPointerDown = (event: PointerEvent) => {
      const target = event.target;
      if (!closeOnOutside || !(target instanceof Node)) return;
      if (container?.contains(target) === true || opener?.contains(target) === true) return;
      onCloseRef.current();
    };
    document.addEventListener('keydown', onKeyDown);
    document.addEventListener('pointerdown', onPointerDown);
    return () => {
      document.removeEventListener('keydown', onKeyDown);
      document.removeEventListener('pointerdown', onPointerDown);
      if (opener?.isConnected === true) opener.focus();
    };
  }, [open, trap, closeOnOutside, openerRef]);

  return ref;
}
