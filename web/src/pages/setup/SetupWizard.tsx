import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useLocation } from 'react-router';
import { useI18n } from '../../shared';
import { resolveEntry } from './resolveEntry';
import { useSetupData } from './useSetupData';
import type { SetupPage, SetupPageEvent } from './types';
import { SetupShell } from './SetupShell';
import { indexPages, requirePage, transition } from './wizardNavigation';
import type { WizardRoute, WizardView } from './types';

type SetupWizardProps = { registry: readonly SetupPage[] };

/** Runs resolved setup pages and delegates all page UI to the shared shell. */
export function SetupWizard({ registry }: SetupWizardProps) {
  const data = useSetupData();
  const location = useLocation();
  const [route, setRoute] = useState<WizardRoute>(initialRoute());
  const routeRef = useRef(route);
  const [liveText, setLiveText] = useState('');
  const [rollbackEnabled, setRollbackEnabled] = useState(true);
  const pages = useMemo(() => indexPages(registry), [registry]);
  const view = wizardView(data);
  const page = route.pageId ? requirePage(pages, route.pageId, 'entry') : null;

  useEntry({ data, search: location.search, view, routeRef, setRoute });
  const report = usePageReporter(pages, routeRef, setRoute, setRollbackEnabled);
  const announce = useCallback((message: string) => setLiveText(message), []);
  const updateRollbackEnabled = useCallback((enabled: boolean) => {
    setRollbackEnabled(enabled);
  }, []);
  useDocumentTitle();

  return (
    <SetupShell
      data={data}
      view={view}
      page={page}
      route={route}
      pages={registry}
      report={report}
      rollbackEnabled={rollbackEnabled}
      setRollbackEnabled={updateRollbackEnabled}
      announce={announce}
      liveText={liveText}
    />
  );
}

function initialRoute(): WizardRoute {
  return { pageId: null, failure: null, payload: null, hostError: null, revision: 0 };
}

function wizardView(data: ReturnType<typeof useSetupData>): WizardView {
  if (data.initialLoading) return 'loading';
  if (data.forbidden) return 'forbidden';
  return data.loadFailed ? 'error' : 'steps';
}

function useEntry({
  data,
  search,
  view,
  routeRef,
  setRoute,
}: {
  data: ReturnType<typeof useSetupData>;
  search: string;
  view: WizardView;
  routeRef: React.RefObject<WizardRoute>;
  setRoute: (route: WizardRoute) => void;
}) {
  const resolved = useRef(false);
  useEffect(() => {
    if (resolved.current || view !== 'steps') return;
    resolved.current = true;
    const route = { ...initialRoute(), pageId: resolveEntry(data, search) };
    routeRef.current = route;
    setRoute(route);
  }, [data, routeRef, search, setRoute, view]);
}

function usePageReporter(
  pages: Map<string, SetupPage>,
  routeRef: React.RefObject<WizardRoute>,
  setRoute: (route: WizardRoute) => void,
  setRollbackEnabled: (enabled: boolean) => void,
) {
  return useCallback(
    (event: SetupPageEvent) => {
      try {
        const route = transition(pages, routeRef.current, event);
        routeRef.current = route;
        setRollbackEnabled(true);
        setRoute(route);
      } catch (error) {
        const current = routeRef.current;
        const route = {
          ...current,
          hostError: error instanceof Error ? error.message : String(error),
          revision: current.revision + 1,
        };
        routeRef.current = route;
        setRoute(route);
      }
    },
    [pages, routeRef, setRollbackEnabled, setRoute],
  );
}

function useDocumentTitle() {
  const { t } = useI18n();
  useEffect(() => {
    document.title = `${t('setup.title')} · XFlickr`;
  }, [t]);
}
