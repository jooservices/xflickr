import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useCallback, useEffect, useRef, useState } from 'react';
import { Link, useSearchParams } from 'react-router';
import { Icon, useI18n, useLoadingTiming } from '../../shared';
import type { MessageKey } from '../../shared/i18n';
import type { ApiKey } from '../../shared/api/gen/models';
import { loadAccounts, loadKeys } from '../../features/keys/api';
import { loadUsage, removeKey, setKeyPaused, startConnect } from './api';
import { leaveTo } from './navigate';
import type { UsageSnapshot } from './api';
import { AddKeyDialog } from './AddKeyDialog';
import { KeyCard } from './KeyCard';
import { RemoveKeyDialog } from './RemoveKeyDialog';
import { msToReset } from './format';
import styles from './keys.module.css';

const SKELETON_DELAY_MS = 150;
/** Polling while the live stream is not available: 10, 20, 40, 60 s. */
const POLL_MS = [10_000, 20_000, 40_000, 60_000];
const ACCOUNTS_POLL_MS = 30_000;
const TOAST_MS = 3000;

const statusOf = (error: unknown) => (error as { status?: number } | null)?.status;
const KEYS = ['/keys'] as const;
const USAGE = ['/keys/usage'] as const;
const ACCOUNTS = ['/flickr-accounts'] as const;

export function KeysPage() {
  const { t, locale } = useI18n();
  const queryClient = useQueryClient();
  const [params, setParams] = useSearchParams();
  const heading = useRef<HTMLHeadingElement>(null);
  const addButton = useRef<HTMLButtonElement>(null);
  const emptyCta = useRef<HTMLButtonElement>(null);
  const block = useRef<HTMLDivElement>(null);
  const cards = useRef(new Map<number, HTMLHeadingElement>());
  const resetFetched = useRef(new Set<string>());
  const previousStates = useRef(new Map<number, string>());
  const busy = useRef(new Set<number>());

  const [addOpen, setAddOpen] = useState(params.get('add') === '1');
  const [removing, setRemoving] = useState<ApiKey | null>(null);
  const [pending, setPending] = useState<number[]>([]);
  const [connecting, setConnecting] = useState(false);
  const [focusCard, setFocusCard] = useState<number | null>(null);
  const [announcement, setAnnouncement] = useState('');
  const [now, setNow] = useState(() => Date.now());
  const [toasts, setToasts] = useState<{ id: number; text: string }[]>([]);
  const nextToast = useRef(1);

  const toast = useCallback((text: string) => {
    const id = nextToast.current;
    nextToast.current += 1;
    setToasts((current) => [...current, { id, text }]);
    window.setTimeout(
      () => setToasts((current) => current.filter((item) => item.id !== id)),
      TOAST_MS,
    );
  }, []);

  const keys = useQuery({
    queryKey: KEYS,
    queryFn: ({ signal }) => loadKeys(signal),
    retry: false,
  });
  const hasKeys = (keys.data?.length ?? 0) > 0;
  const usage = useQuery({
    queryKey: USAGE,
    queryFn: ({ signal }) => loadUsage(signal),
    enabled: hasKeys,
    retry: false,
    refetchInterval: (query) => POLL_MS[Math.min(query.state.errorUpdateCount, POLL_MS.length - 1)],
  });
  const accounts = useQuery({
    queryKey: ACCOUNTS,
    queryFn: ({ signal }) => loadAccounts(signal),
    enabled: hasKeys,
    retry: false,
    refetchInterval: ACCOUNTS_POLL_MS,
  });

  useEffect(() => {
    document.title = t('shell.document-title', { view: t('keys.title') });
  }, [t]);

  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, []);

  const refetchUsage = usage.refetch;

  const snapshot = usage.data;
  const received = usage.dataUpdatedAt;
  const stale = usage.isError;

  // When a countdown reaches zero fetch once at once and again after 3 s.
  useEffect(() => {
    if (snapshot === undefined) return;
    for (const item of snapshot.items) {
      const left = msToReset(item.window_resets_at, snapshot.server_time, received, now);
      const mark = `${String(item.key_id)}:${item.window_resets_at}`;
      if (left > 0 || resetFetched.current.has(mark)) continue;
      resetFetched.current.add(mark);
      void refetchUsage();
      window.setTimeout(() => void refetchUsage(), 3000);
    }
  }, [snapshot, received, now, refetchUsage]);

  // Announce a changed state once through the polite region.
  useEffect(() => {
    if (snapshot === undefined || keys.data === undefined) return;
    for (const item of snapshot.items) {
      const before = previousStates.current.get(item.key_id);
      previousStates.current.set(item.key_id, item.state);
      const label = keys.data.find((key) => key.id === item.key_id)?.label;
      if (before !== undefined && before !== item.state && label !== undefined)
        setAnnouncement(
          t('keys.live.state-changed', {
            label,
            state: t(`keys.state.${item.state}` as MessageKey),
          }),
        );
    }
  }, [snapshot, keys.data, t]);

  const failed = keys.isError;
  const forbidden = failed && statusOf(keys.error) === 403;
  const loading = keys.isPending || (hasKeys && usage.isPending && !usage.isError);
  const skeleton = useLoadingTiming(loading, SKELETON_DELAY_MS);

  useEffect(() => {
    if (focusCard === null) return;
    const target = cards.current.get(focusCard);
    if (target) {
      target.focus();
      setFocusCard(null);
    }
  }, [focusCard, keys.data, loading]);

  useEffect(() => {
    if (failed) block.current?.focus();
  }, [failed]);

  const closeAdd = useCallback(() => {
    setAddOpen(false);
    if (params.has('add')) {
      const next = new URLSearchParams(params);
      next.delete('add');
      setParams(next, { replace: true });
    }
  }, [params, setParams]);

  const onAdded = async (key: ApiKey) => {
    queryClient.setQueryData<ApiKey[]>(KEYS, (current) => [...(current ?? []), key]);
    closeAdd();
    setFocusCard(key.id);
    let node = '–';
    try {
      const fresh = await queryClient.fetchQuery({
        queryKey: USAGE,
        queryFn: ({ signal }) => loadUsage(signal),
        staleTime: 0,
      });
      node = fresh.items.find((item) => item.key_id === key.id)?.node_id ?? node;
    } catch {
      // Usage fills in on the next poll.
    }
    void queryClient.invalidateQueries({ queryKey: ACCOUNTS });
    toast(t('keys.toast.added', { node }));
  };

  const guard = async (id: number, action: () => Promise<void>) => {
    if (busy.current.has(id)) return;
    busy.current.add(id);
    setPending(Array.from(busy.current));
    try {
      await action();
    } catch {
      toast(t('keys.error.action-failed'));
    } finally {
      busy.current.delete(id);
      setPending(Array.from(busy.current));
    }
  };

  const togglePause = (key: ApiKey, paused: boolean) =>
    guard(key.id, async () => {
      await setKeyPaused(key.id, paused);
      queryClient.setQueryData<UsageSnapshot>(USAGE, (current) =>
        current === undefined
          ? current
          : {
              ...current,
              items: current.items.map((item) =>
                item.key_id === key.id ? { ...item, paused } : item,
              ),
            },
      );
      void queryClient.invalidateQueries({ queryKey: USAGE });
      toast(t(paused ? 'keys.toast.paused' : 'keys.toast.resumed', { label: key.label }));
    });

  const confirmRemove = () => {
    const key = removing;
    if (key === null) return;
    void guard(key.id, async () => {
      await removeKey(key.id);
      const rest = (queryClient.getQueryData<ApiKey[]>(KEYS) ?? []).filter(
        (item) => item.id !== key.id,
      );
      queryClient.setQueryData<ApiKey[]>(KEYS, rest);
      void queryClient.invalidateQueries({ queryKey: USAGE });
      void queryClient.invalidateQueries({ queryKey: ACCOUNTS });
      setRemoving(null);
      toast(t('keys.toast.removed'));
      window.setTimeout(() => (rest.length === 0 ? emptyCta : addButton).current?.focus(), 0);
    });
  };

  const connect = async (keyId: number) => {
    if (connecting) return;
    setConnecting(true);
    try {
      leaveTo(await startConnect(keyId));
    } catch {
      toast(t('keys.error.reconnect-failed'));
    } finally {
      setConnecting(false);
    }
  };

  const retryFailed = () => {
    if (keys.isError) void keys.refetch();
    if (usage.isError) void usage.refetch();
  };

  const time = (stamp: number) =>
    new Intl.DateTimeFormat(locale, { timeStyle: 'medium' }).format(new Date(stamp));
  const list = keys.data ?? [];

  let content;
  if (forbidden) {
    content = (
      <div className={styles.block} data-testid="keys-forbidden">
        <Icon name="lock" size={36} />
        <h2>{t('keys.forbidden.title')}</h2>
        <p>{t('keys.forbidden.text')}</p>
        <Link to="/" className={styles.primary} data-testid="keys-forbidden-cta">
          {t('keys.forbidden.cta')}
        </Link>
      </div>
    );
  } else if (failed) {
    content = (
      <div ref={block} tabIndex={-1} className={styles.block} data-testid="keys-error">
        <Icon name="alert" size={36} />
        <h2>{t('keys.error.title')}</h2>
        <p>{t('keys.error.text')}</p>
        <button
          type="button"
          className={styles.primary}
          onClick={retryFailed}
          data-testid="keys-error-retry"
        >
          {t('common.retry')}
        </button>
      </div>
    );
  } else if (loading) {
    content = skeleton ? (
      <ul className={styles.grid} aria-busy="true" data-testid="keys-skeleton">
        {[1, 2, 3].map((item) => (
          <li key={item} className={styles.card} aria-hidden="true">
            <div className={styles.shimmer} />
            <div className={styles.shimmer} />
            <div className={styles.shimmer} />
            <div className={styles.shimmer} />
          </li>
        ))}
      </ul>
    ) : null;
  } else if (list.length === 0) {
    content = (
      <div className={styles.block} data-testid="keys-empty">
        <Icon name="key" size={36} />
        <h2>{t('keys.empty.title')}</h2>
        <p>{t('keys.empty.text')}</p>
        <button
          ref={emptyCta}
          type="button"
          className={styles.primary}
          onClick={() => setAddOpen(true)}
          data-testid="keys-empty-cta"
        >
          {t('keys.empty.cta')}
        </button>
      </div>
    );
  } else {
    const items = new Map(snapshot?.items.map((item) => [item.key_id, item]));
    content = (
      <>
        {stale ? (
          <div className={styles.staleBanner} role="status" data-testid="keys-stale-banner">
            <Icon name="alert" size={16} />
            <span>{t('keys.stale.usage', { time: time(received || now) })}</span>
            <button
              type="button"
              className={styles.ghostSmall}
              onClick={retryFailed}
              data-testid="keys-stale-retry"
            >
              {t('common.retry')}
            </button>
          </div>
        ) : null}
        <ul className={styles.grid} data-testid="keys-grid">
          {list.map((key) => {
            const usageItem = items.get(key.id);
            return (
              <li key={key.id}>
                <KeyCard
                  apiKey={key}
                  usage={usageItem}
                  resetMs={
                    usageItem === undefined || snapshot === undefined || stale
                      ? null
                      : msToReset(usageItem.window_resets_at, snapshot.server_time, received, now)
                  }
                  accounts={accounts.data}
                  accountsFailed={accounts.isError}
                  busy={pending.includes(key.id)}
                  connecting={connecting}
                  heading={(element) => {
                    if (element) cards.current.set(key.id, element);
                    else cards.current.delete(key.id);
                  }}
                  onToggle={(target, paused) => void togglePause(target, paused)}
                  onRemove={setRemoving}
                  onConnect={(keyId) => void connect(keyId)}
                  onRetryAccounts={() => void accounts.refetch()}
                />
              </li>
            );
          })}
        </ul>
        <div className={styles.note} role="note" data-testid="keys-note">
          <Icon name="info" size={16} />
          <p>{t('keys.note')}</p>
        </div>
      </>
    );
  }

  return (
    <div className={styles.page} data-testid="keys-page">
      <header className={styles.head}>
        <div>
          <h1 ref={heading} tabIndex={-1} data-testid="keys-title">
            {t('keys.title')}
          </h1>
          <p className={styles.lead} data-testid="keys-lead">
            {t('keys.lead')}
          </p>
        </div>
        <button
          ref={addButton}
          type="button"
          className={styles.primary}
          onClick={() => setAddOpen(true)}
          data-testid="keys-add"
        >
          <Icon name="plus" size={16} /> {t('keys.add')}
        </button>
      </header>
      {content}
      <div className={styles.toasts} aria-live="polite" data-testid="keys-toast-region">
        {toasts.map((item) => (
          <div key={item.id} className={styles.toast} data-testid="keys-toast">
            {item.text}
          </div>
        ))}
      </div>
      <div
        className={styles.srOnly}
        role="status"
        aria-live="polite"
        data-testid="keys-live-region"
      >
        {announcement}
      </div>
      {addOpen ? <AddKeyDialog onAdded={(key) => void onAdded(key)} onClose={closeAdd} /> : null}
      {removing === null ? null : (
        <RemoveKeyDialog
          label={removing.label}
          accounts={(accounts.data ?? []).filter((item) => item.key_id === removing.id).length}
          busy={pending.includes(removing.id)}
          onConfirm={confirmRemove}
          onCancel={() => setRemoving(null)}
        />
      )}
    </div>
  );
}
