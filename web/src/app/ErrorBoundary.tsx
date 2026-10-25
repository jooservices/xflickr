import { Component, useEffect, useMemo } from 'react';
import type { ErrorInfo, ReactNode } from 'react';
import { useNavigate, useRouteError } from 'react-router';
import { useI18n } from '../shared';
import { ApiShapeError } from '../shared/api/mutator';
import { newCorrelationId } from '../shared/api/validate';
import { Icon } from '../shared/ui/icon';
import styles from '../shared/ui/states/States.module.css';

function reportError(correlationId: string, error: unknown, componentStack?: string | null) {
  console.error('XFlickr application error', { correlationId, error, componentStack });
}

/** One correlation id per caught error: shown to the person and written to the report. */
function ErrorPage({
  error,
  correlationId: given,
  reset,
}: {
  error?: unknown;
  correlationId?: string;
  reset?: () => void;
}) {
  const { t } = useI18n();
  const navigate = useNavigate();
  const correlationId = useMemo(
    () => given ?? (error instanceof ApiShapeError ? error.correlationId : newCorrelationId()),
    [given, error],
  );
  useEffect(() => {
    if (given === undefined) reportError(correlationId, error);
  }, [given, correlationId, error]);
  return (
    <section role="alert" data-testid="global-error" className={styles.block}>
      <Icon name="alert" size={30} />
      <h2>{t('shell.global-error.title')}</h2>
      <p>{t('shell.global-error.text')}</p>
      <small>{t('shell.global-error.correlation', { id: correlationId })}</small>
      <div className={styles.actions}>
        {reset ? (
          <button className={styles.button} onClick={reset}>
            {t('common.retry')}
          </button>
        ) : null}
        <button className={`${styles.button} ${styles.primary}`} onClick={() => void navigate('/')}>
          {t('shell.notfound.home')}
        </button>
      </div>
    </section>
  );
}

export function RouteErrorPage() {
  return <ErrorPage error={useRouteError()} />;
}

type BoundaryState = { failed: boolean; error?: unknown; correlationId?: string };

export class AppErrorBoundary extends Component<{ children: ReactNode }, BoundaryState> {
  state: BoundaryState = { failed: false };

  static getDerivedStateFromError(error: unknown): BoundaryState {
    return {
      failed: true,
      error,
      correlationId: error instanceof ApiShapeError ? error.correlationId : newCorrelationId(),
    };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    reportError(this.state.correlationId ?? newCorrelationId(), error, info.componentStack);
  }

  render() {
    return this.state.failed ? (
      <ErrorPage
        error={this.state.error}
        correlationId={this.state.correlationId}
        reset={() => this.setState({ failed: false, error: undefined, correlationId: undefined })}
      />
    ) : (
      this.props.children
    );
  }
}
