import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react';
import type { ReactNode } from 'react';
import { useI18n } from '../../shared';
import styles from './shells.module.css';

type Toast = { id: number; text: string; actionLabel?: string; onAction?: () => void };

type ShellRuntime = {
  /** The user paused live data. */
  livePaused: boolean;
  /** Live is effectively off: paused by the user or offline. */
  liveOff: boolean;
  offline: boolean;
  toggleLive: () => void;
  toast: (text: string, action?: { label: string; run: () => void }) => void;
};

const RuntimeContext = createContext<ShellRuntime | null>(null);

export const TOAST_MS = 2600;
export const UNDO_TOAST_MS = 8000;

function isOffline(): boolean {
  return typeof navigator !== 'undefined' && !navigator.onLine;
}

/**
 * Live/paused state, online/offline detection, the toast stack and the polite
 * announcement region shared by both shells.
 */
export function ShellRuntimeProvider({ children }: { children: ReactNode }) {
  const { t } = useI18n();
  const [livePaused, setLivePaused] = useState(false);
  const [offline, setOffline] = useState(isOffline);
  const [toasts, setToasts] = useState<Toast[]>([]);
  const [announcement, setAnnouncement] = useState('');
  const nextId = useRef(1);

  const toast = useCallback((text: string, action?: { label: string; run: () => void }) => {
    const id = nextId.current;
    nextId.current += 1;
    setToasts((current) => [
      ...current,
      { id, text, actionLabel: action?.label, onAction: action?.run },
    ]);
    window.setTimeout(
      () => {
        setToasts((current) => current.filter((item) => item.id !== id));
      },
      action === undefined ? TOAST_MS : UNDO_TOAST_MS,
    );
  }, []);

  const toggleLive = useCallback(() => {
    const next = !livePaused;
    setLivePaused(next);
    const message = next ? t('shell.live.paused-toast') : t('shell.live.resumed-toast');
    setAnnouncement(next ? t('shell.a11y.live-paused') : t('shell.a11y.live-resumed'));
    toast(message);
  }, [livePaused, t, toast]);

  useEffect(() => {
    const goOffline = () => {
      setOffline(true);
      setAnnouncement(t('shell.a11y.offline'));
    };
    const goOnline = () => {
      setOffline(false);
      setAnnouncement(t('shell.a11y.online'));
      toast(t('shell.offline.back'));
    };
    window.addEventListener('offline', goOffline);
    window.addEventListener('online', goOnline);
    return () => {
      window.removeEventListener('offline', goOffline);
      window.removeEventListener('online', goOnline);
    };
  }, [t, toast]);

  const value = useMemo<ShellRuntime>(
    () => ({ livePaused, liveOff: livePaused || offline, offline, toggleLive, toast }),
    [livePaused, offline, toggleLive, toast],
  );

  return (
    <RuntimeContext.Provider value={value}>
      {children}
      <div className={styles.toasts} aria-live="polite" data-testid="shell-toast-region">
        {toasts.map((item) => (
          <div key={item.id} className={styles.toast} data-testid="shell-toast">
            <span>{item.text}</span>
            {item.onAction === undefined ? null : (
              <button
                type="button"
                className={styles.toastAction}
                onClick={item.onAction}
                data-testid="shell-tray-undo"
              >
                {item.actionLabel}
              </button>
            )}
          </div>
        ))}
      </div>
      <div className={styles.srOnly} role="status" aria-live="polite">
        {announcement}
      </div>
    </RuntimeContext.Provider>
  );
}

export function useShellRuntime(): ShellRuntime {
  const value = useContext(RuntimeContext);
  if (value === null) {
    throw new Error('useShellRuntime must be used inside ShellRuntimeProvider');
  }
  return value;
}
