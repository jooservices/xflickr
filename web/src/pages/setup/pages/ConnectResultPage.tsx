import { useEffect, useRef, useState } from 'react';
import { useLocation, useNavigate } from 'react-router';
import { useI18n } from '../../../shared';
import type { ConnectError } from '../setupModel';
import type { SetupPageComponentProps } from '../types';
import styles from '../SetupPage.module.css';

type ConnectResult =
  | { type: 'success'; payload: { connectedId: number | null; connectedMarker: boolean } }
  | { type: 'failure'; error: ConnectError }
  | null;

/** Reads one Flickr return, cleans its parameters and reports its result to the host. */
export function ConnectResultPage({ report }: SetupPageComponentProps) {
  const { t } = useI18n();
  const location = useLocation();
  const navigate = useNavigate();
  const [result] = useState(() => readResult(location.search));
  const handled = useRef(false);

  useEffect(() => {
    cleanReturnUrl(location.pathname, location.search, navigate);
    if (handled.current || !result) return;
    handled.current = true;
    report(result.type === 'success' ? result : { type: 'failure', error: result.error });
  }, [location.pathname, location.search, navigate, report, result]);

  return (
    <p role="status" className={styles.note} data-testid="wizard-connect-waiting">
      {t('setup.connect.waiting')}
    </p>
  );
}

function readResult(search: string): ConnectResult {
  const params = new URLSearchParams(search);
  if (params.has('connected')) {
    const marker = params.get('connected') ?? '';
    return {
      type: 'success',
      payload: { connectedId: Number(marker) || null, connectedMarker: marker === '1' },
    };
  }
  if (params.has('connect_error') || params.has('error')) {
    return {
      type: 'failure',
      error: errorKind(params.get('connect_error') ?? params.get('error')),
    };
  }
  return null;
}

function errorKind(code: string | null): ConnectError {
  if (code === 'denied' || code === 'authorization_denied') return 'denied';
  if (code === 'expired' || code === 'invalid_state') return 'expired';
  return 'start';
}

function cleanReturnUrl(
  pathname: string,
  search: string,
  navigate: ReturnType<typeof useNavigate>,
) {
  const params = new URLSearchParams(search);
  params.delete('connected');
  params.delete('connect_error');
  params.delete('error');
  const query = params.size ? `?${params.toString()}` : '';
  if (query !== search) void navigate(`${pathname}${query}`, { replace: true });
}
