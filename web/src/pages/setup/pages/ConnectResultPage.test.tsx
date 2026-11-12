import { render, waitFor } from '@testing-library/react';
import { I18nextProvider } from 'react-i18next';
import { RouterProvider, createMemoryRouter } from 'react-router';
import { describe, expect, it, vi } from 'vitest';
import { I18nBridge, i18nInstance } from '../../../shared';
import type { SetupPageComponentProps } from '../types';
import { ConnectResultPage } from './ConnectResultPage';

function renderResult(url: string) {
  const report = vi.fn<SetupPageComponentProps['report']>();
  const router = createMemoryRouter(
    [
      {
        path: '/onboarding',
        element: (
          <ConnectResultPage report={report} failure={null} payload={null} announce={vi.fn()} />
        ),
      },
    ],
    { initialEntries: [url] },
  );
  render(
    <I18nextProvider i18n={i18nInstance}>
      <I18nBridge initialLocale="en">
        <RouterProvider router={router} />
      </I18nBridge>
    </I18nextProvider>,
  );
  return { report, router };
}

describe('ConnectResultPage', () => {
  it.each([
    ['?connect_error=denied', 'denied'],
    ['?error=authorization_denied', 'denied'],
    ['?connect_error=expired', 'expired'],
    ['?error=invalid_state', 'expired'],
    ['?error=flickr_unavailable', 'start'],
  ])(
    'reports a %s redirect as a %s failure and cleans the result parameters',
    async (query, error) => {
      const { report, router } = renderResult(`/onboarding?keep=1&${query.slice(1)}`);

      await waitFor(() => expect(report).toHaveBeenCalledWith({ type: 'failure', error }));
      await waitFor(() => expect(router.state.location.search).toBe('?keep=1'));
    },
  );

  it('reports a successful Flickr return and removes its parameters', async () => {
    const { report, router } = renderResult('/onboarding?connected=1&step=2');

    await waitFor(() =>
      expect(report).toHaveBeenCalledWith({
        type: 'success',
        payload: { connectedId: 1, connectedMarker: true },
      }),
    );
    await waitFor(() => expect(router.state.location.search).toBe('?step=2'));
  });
});
