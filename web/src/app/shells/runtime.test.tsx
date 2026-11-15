import { act, renderHook } from '@testing-library/react';
import type { ReactNode } from 'react';
import { I18nextProvider } from 'react-i18next';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { I18nBridge, i18nInstance } from '../../shared';
import { ShellRuntimeProvider, TOAST_MS, useShellRuntime } from './runtime';

function wrapper({ children }: { children: ReactNode }) {
  return (
    <I18nextProvider i18n={i18nInstance}>
      <I18nBridge initialLocale="en">
        <ShellRuntimeProvider>{children}</ShellRuntimeProvider>
      </I18nBridge>
    </I18nextProvider>
  );
}

describe('shell runtime', () => {
  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it('rejects consumers outside the provider', () => {
    expect(() => renderHook(() => useShellRuntime())).toThrow(
      'useShellRuntime must be used inside ShellRuntimeProvider',
    );
  });

  it('cancels the pending toast timers when the provider unmounts', () => {
    vi.useFakeTimers();
    const clear = vi.spyOn(window, 'clearTimeout');
    const { result, unmount } = renderHook(() => useShellRuntime(), { wrapper });
    act(() => result.current.toast('saved'));
    act(() => result.current.toast('undo me', { label: 'Undo', run: () => undefined }));
    unmount();
    expect(clear).toHaveBeenCalledTimes(2);
    expect(() => vi.advanceTimersByTime(TOAST_MS * 10)).not.toThrow();
  });

  it('forgets a toast timer once it fired', () => {
    vi.useFakeTimers();
    const clear = vi.spyOn(window, 'clearTimeout');
    const { result, unmount } = renderHook(() => useShellRuntime(), { wrapper });
    act(() => result.current.toast('saved'));
    act(() => {
      vi.advanceTimersByTime(TOAST_MS + 1);
    });
    unmount();
    expect(clear).not.toHaveBeenCalled();
  });
});
