import { useCallback, useEffect, useLayoutEffect, useRef } from 'react';
import type { RefObject } from 'react';
import { Link } from 'react-router';
import { Icon, useI18n } from '../../shared';
import { PALETTE, tileBackground } from './setupModel';
import type {
  SetupPage,
  SetupPageEvent,
  SetupPageReporter,
  SetupPageRollbackSetter,
  WizardRoute,
  WizardView,
} from './types';
import { useSetupData } from './useSetupData';
import styles from './SetupPage.module.css';

export type SetupShellProps = {
  data: ReturnType<typeof useSetupData>;
  view: WizardView;
  page: SetupPage | null;
  route: WizardRoute;
  pages: readonly SetupPage[];
  report: SetupPageReporter;
  rollbackEnabled: boolean;
  setRollbackEnabled: SetupPageRollbackSetter;
  announce: (message: string) => void;
  liveText: string;
};

/** Shared shell for page title, focus, progress, loading and live announcements. */
export function SetupShell(props: SetupShellProps) {
  return (
    <section className={styles.page} data-testid="wizard-page">
      <div className={styles.card}>
        <WizardArt />
        <div
          className={styles.body}
          data-testid="wizard-body"
          aria-busy={props.data.initialLoading}
        >
          <WizardBody {...props} />
        </div>
      </div>
    </section>
  );
}

function WizardArt() {
  return (
    <div className={styles.art} aria-hidden="true" data-testid="wizard-art">
      {Array.from({ length: 18 }, (_, index) => (
        <i key={index} style={{ background: tileBackground(PALETTE, index) }} />
      ))}
    </div>
  );
}

function WizardBody(props: SetupShellProps) {
  if (props.view !== 'steps') return <WizardStatus data={props.data} view={props.view} />;
  if (!props.page) return null;
  return <ReadyBody {...props} page={props.page} />;
}

function WizardStatus({ data, view }: { data: SetupShellProps['data']; view: WizardView }) {
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => {
    if (view !== 'loading') heading.current?.focus();
  }, [view]);
  if (view === 'loading') return <LoadingState visible={data.showSkeleton} />;
  if (view === 'forbidden') return <ForbiddenState heading={heading} />;
  return <LoadErrorState heading={heading} retry={data.retryLoad} />;
}

function LoadingState({ visible }: { visible: boolean }) {
  const { t } = useI18n();
  if (!visible) return null;
  return (
    <div className={styles.loading} role="status" data-testid="wizard-skeleton">
      <span>{t('setup.loading')}</span>
      <i className={styles.skBar} />
      <i className={styles.skHead} />
      <i className={styles.skLine} />
      <i className={styles.skLine} />
      <i className={styles.skField} />
    </div>
  );
}

function ForbiddenState({ heading }: { heading: RefObject<HTMLHeadingElement | null> }) {
  const { t } = useI18n();
  return (
    <div className={styles.state} data-testid="wizard-forbidden">
      <Icon name="lock" size={28} />
      <h1 ref={heading} tabIndex={-1} data-testid="wizard-title">
        {t('setup.forbidden.title')}
      </h1>
      <p>{t('setup.forbidden.text')}</p>
      <Link className={styles.primary} to="/" data-testid="wizard-forbidden-cta">
        {t('setup.forbidden.cta')}
      </Link>
    </div>
  );
}

function LoadErrorState({
  heading,
  retry,
}: {
  heading: RefObject<HTMLHeadingElement | null>;
  retry: () => void;
}) {
  const { t } = useI18n();
  return (
    <div className={styles.state} data-testid="wizard-error">
      <Icon name="alert" size={28} />
      <h1 ref={heading} tabIndex={-1} data-testid="wizard-title">
        {t('setup.error.title')}
      </h1>
      <p>{t('setup.error.text')}</p>
      <button className={styles.primary} onClick={retry} data-testid="wizard-error-retry">
        {t('common.retry')}
      </button>
    </div>
  );
}

function ReadyBody({ ...props }: SetupShellProps & { page: SetupPage }) {
  const { t } = useI18n();
  const heading = useRef<HTMLHeadingElement>(null);
  const content = useRef<HTMLDivElement>(null);
  const progress = useProgress(props.pages, props.page);
  usePageFocus(content, heading, props.page.id, props.route.revision);
  const { page, route, report: reportPage, setRollbackEnabled: setPageRollbackEnabled } = props;
  const PageComponent = page.component;
  const report = useCallback(
    (event: SetupPageEvent) => reportPage(page.id, route.pageVisit, event),
    [page.id, route.pageVisit, reportPage],
  );
  const setRollbackEnabled = useCallback(
    (enabled: boolean) => setPageRollbackEnabled(page.id, route.pageVisit, enabled),
    [page.id, route.pageVisit, setPageRollbackEnabled],
  );
  return (
    <>
      <WizardProgress {...progress} />
      <p className={styles.eyebrow} data-testid="wizard-eyebrow">
        {progress.stepText}
      </p>
      {props.data.staleAccounts ? <StaleAccounts data={props.data} /> : null}
      <h1 ref={heading} tabIndex={-1} data-testid="wizard-title">
        {t(props.page.title(props.data))}
      </h1>
      {props.route.hostError ? (
        <p role="alert" data-testid="wizard-host-error">
          {props.route.hostError}
        </p>
      ) : null}
      <div ref={content}>
        <PageComponent
          rollback={
            <RollbackActions
              page={props.page}
              data={props.data}
              report={report}
              enabled={props.rollbackEnabled}
            />
          }
          setRollbackEnabled={setRollbackEnabled}
          report={report}
          failure={props.route.failure}
          payload={props.route.payload}
          announce={props.announce}
        />
      </div>
      <LiveRegion text={props.liveText} />
      <KeysLink />
    </>
  );
}

function RollbackActions({
  page,
  data,
  report,
  enabled,
}: {
  page: SetupPage;
  data: SetupShellProps['data'];
  report: (event: SetupPageEvent) => void;
  enabled: boolean;
}) {
  const { t } = useI18n();
  const targets = page.rollbackTo.filter((target) => page.showRollback(data, target));
  if (!enabled || targets.length === 0) return null;
  return (
    <>
      {targets.map((target) => (
        <button
          key={target}
          type="button"
          className={styles.ghost}
          onClick={() => report({ type: 'rollback', target })}
          data-testid={`wizard-${page.id}-back${targets.length > 1 ? `-${target}` : ''}`}
        >
          {t('setup.connect.back')}
        </button>
      ))}
    </>
  );
}

function useProgress(pages: readonly SetupPage[], page: SetupPage) {
  const { t } = useI18n();
  const progressPages = pages.filter((item) => item.visibleInProgress);
  const index = pages.findIndex((item) => item.id === page.id);
  const end = page.visibleInProgress ? index + 1 : index;
  const step = Math.max(1, pages.slice(0, end).filter((item) => item.visibleInProgress).length);
  const stepPage = progressPages[Math.max(0, step - 1)] ?? page;
  const stepText = t('setup.step', {
    n: step,
    total: progressPages.length,
    name: t(stepPage.progressName),
  });
  return { pages: progressPages, step, stepText };
}

function usePageFocus(
  content: RefObject<HTMLDivElement | null>,
  heading: RefObject<HTMLHeadingElement | null>,
  pageId: string,
  revision: number,
) {
  useLayoutEffect(() => {
    const target =
      content.current?.querySelector<HTMLElement>('[data-setup-focus]') ?? heading.current;
    target?.focus();
  }, [pageId, revision, content, heading]);
}

function WizardProgress({
  pages,
  step,
  stepText,
}: {
  pages: SetupPage[];
  step: number;
  stepText: string;
}) {
  const { t } = useI18n();
  return (
    <div
      className={styles.progress}
      role="progressbar"
      aria-valuenow={step}
      aria-valuemin={1}
      aria-valuemax={pages.length}
      aria-valuetext={stepText}
      aria-label={t('setup.steps.aria')}
      data-testid="wizard-steps"
    >
      {pages.map((page, index) => (
        <i key={page.id} className={index < step ? styles.filled : ''} />
      ))}
    </div>
  );
}

function StaleAccounts({ data }: { data: SetupShellProps['data'] }) {
  const { t } = useI18n();
  return (
    <div role="status" className={styles.warn} data-testid="wizard-stale-banner">
      <span>{t('setup.stale.accounts')}</span>
      <button
        type="button"
        className={styles.ghost}
        onClick={() => void data.refetchAccounts()}
        data-testid="wizard-stale-retry"
      >
        {t('common.retry')}
      </button>
    </div>
  );
}

function LiveRegion({ text }: { text: string }) {
  return (
    <div
      className={styles.announce}
      role="status"
      aria-live="polite"
      data-testid="wizard-live-region"
    >
      {text}
    </div>
  );
}

function KeysLink() {
  const { t } = useI18n();
  return (
    <Link className={styles.keysLink} to="/keys">
      {t('shell.nav.keys')}
    </Link>
  );
}
