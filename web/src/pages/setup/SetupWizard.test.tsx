import { fireEvent, render, screen } from '@testing-library/react';
import { I18nextProvider } from 'react-i18next';
import { RouterProvider, createMemoryRouter } from 'react-router';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { I18nBridge, i18nInstance } from '../../shared';
import { definePage } from './definePage';
import type { SetupPageComponentProps } from './types';
import { SetupWizard } from './SetupWizard';
import type { SetupData } from './useSetupData';
import type { SetupPage } from './types';

const setupDataMock = vi.hoisted(() => ({ value: null as unknown }));

vi.mock('./useSetupData', () => ({
  useSetupData: () => setupDataMock.value,
}));

function FakeKeyPage({ report, failure, rollback }: SetupPageComponentProps) {
  return (
    <div data-testid="fake-key">
      {failure ? <p data-testid="page-error">{failure}</p> : null}
      <button onClick={() => report({ type: 'success' })}>Continue</button>
      <button onClick={() => report({ type: 'failure', error: 'Page failed' })}>Fail</button>
      {rollback}
    </div>
  );
}

function FakeCustomPage({ report, failure, rollback }: SetupPageComponentProps) {
  return (
    <div data-testid="fake-custom">
      {failure ? <p data-testid="page-error">{failure}</p> : null}
      <button onClick={() => report({ type: 'rollback', target: 'unknown' })}>Bad back</button>
      <button onClick={() => report({ type: 'failure', error: 'Custom failed' })}>Fail</button>
      {rollback}
    </div>
  );
}

function FakeDonePage() {
  return <div data-testid="fake-done" />;
}

function pages(key: SetupPage = keyPage()) {
  return [key, customPage(), donePage()];
}

function keyPage() {
  return definePage({
    id: 'key',
    title: 'setup.key.title',
    progressName: 'setup.step.key',
    next: 'custom',
    rollbackTo: ['done'],
    component: FakeKeyPage,
  });
}

function customPage() {
  return definePage({
    id: 'custom',
    title: 'setup.connect.title',
    progressName: 'setup.step.connect',
    next: 'done',
    onFail: 'key',
    rollbackTo: ['key'],
    component: FakeCustomPage,
  });
}

function donePage() {
  return definePage({
    id: 'done',
    title: 'setup.done.title',
    progressName: 'setup.step.done',
    next: 'done',
    component: FakeDonePage,
  });
}

function renderWizard(registry: SetupPage[]) {
  setupDataMock.value = {
    keys: [],
    accounts: [],
    initialLoading: false,
    hasAccount: false,
    showSkeleton: false,
    forbidden: false,
    loadFailed: false,
    retryLoad: vi.fn(),
    staleAccounts: false,
    refetchAccounts: vi.fn(),
  } as unknown as SetupData;
  const router = createMemoryRouter(
    [{ path: '/onboarding', element: <SetupWizard registry={registry} /> }],
    { initialEntries: ['/onboarding'] },
  );
  return render(
    <I18nextProvider i18n={i18nInstance}>
      <I18nBridge initialLocale="en">
        <RouterProvider router={router} />
      </I18nBridge>
    </I18nextProvider>,
  );
}

afterEach(() => {
  setupDataMock.value = null;
});

describe('SetupWizard registry transitions', () => {
  it('renders a registered page reached through the current page contract', async () => {
    renderWizard(pages());

    fireEvent.click(await screen.findByRole('button', { name: 'Continue' }));

    expect(await screen.findByTestId('fake-custom')).toBeInTheDocument();
    expect(screen.getByTestId('wizard-eyebrow')).toHaveTextContent('Step 2 of 3');
  });

  it('follows onFail and passes the page failure to the destination page', async () => {
    renderWizard(pages());
    fireEvent.click(await screen.findByRole('button', { name: 'Continue' }));
    fireEvent.click(await screen.findByRole('button', { name: 'Fail' }));

    expect(await screen.findByTestId('fake-key')).toBeInTheDocument();
    expect(screen.getByTestId('page-error')).toHaveTextContent('Custom failed');
  });

  it('stays on the page when the default onFail target is the page itself', async () => {
    const stayingPage = definePage({
      id: 'key',
      title: 'setup.key.title',
      progressName: 'setup.step.key',
      next: 'custom',
      component: FakeKeyPage,
    });
    renderWizard(pages(stayingPage));
    fireEvent.click(await screen.findByRole('button', { name: 'Fail' }));

    expect(screen.getByTestId('fake-key')).toBeInTheDocument();
    expect(screen.getByTestId('page-error')).toHaveTextContent('Page failed');
  });
});

describe('SetupWizard rollback validation', () => {
  it('allows rollback only to the source page declared targets', async () => {
    renderWizard(pages());
    fireEvent.click(await screen.findByRole('button', { name: 'Continue' }));
    fireEvent.click(await screen.findByRole('button', { name: 'Back' }));

    expect(await screen.findByTestId('fake-key')).toBeInTheDocument();
  });

  it('rejects a success target that is missing from the registry', async () => {
    const page = definePage({
      id: 'key',
      title: 'setup.key.title',
      progressName: 'setup.step.key',
      next: 'unknown',
      component: FakeKeyPage,
    });
    renderWizard([page]);
    fireEvent.click(await screen.findByRole('button', { name: 'Continue' }));

    expect(await screen.findByTestId('wizard-host-error')).toHaveTextContent(
      'Unknown setup page "unknown"',
    );
  });

  it('rejects a rollback outside rollbackTo with a clear page error', async () => {
    renderWizard(pages());
    fireEvent.click(await screen.findByRole('button', { name: 'Continue' }));
    fireEvent.click(screen.getByRole('button', { name: 'Bad back' }));

    expect(await screen.findByTestId('wizard-host-error')).toHaveTextContent(
      'cannot roll back to "unknown"',
    );
  });

  it('rejects an unknown registered rollback target with a clear error', async () => {
    renderWizard([
      keyPage(),
      definePage({
        id: 'custom',
        title: 'setup.connect.title',
        progressName: 'setup.step.connect',
        next: 'done',
        rollbackTo: ['unknown'],
        component: FakeCustomPage,
      }),
      donePage(),
    ]);
    fireEvent.click(await screen.findByRole('button', { name: 'Continue' }));

    fireEvent.click(screen.getByRole('button', { name: 'Bad back' }));
    expect(await screen.findByTestId('wizard-host-error')).toHaveTextContent(
      'Unknown setup page "unknown"',
    );
    expect(screen.getByTestId('fake-custom')).toBeInTheDocument();
  });
});
