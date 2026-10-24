import { act, fireEvent, render, renderHook, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { I18nProvider } from '../../i18n';
import type { Locale } from '../../i18n';
import {
  EmptyState,
  ErrorState,
  ForbiddenState,
  LoadingState,
  NotFoundState,
  PageState,
  StatePreview,
  useLoadingTiming,
} from '.';
import type { ViewState } from '.';
import type { ReactNode } from 'react';

function withI18n(node: ReactNode, locale: Locale = 'en') {
  return <I18nProvider initialLocale={locale}>{node}</I18nProvider>;
}

describe('LoadingState', () => {
  it('announces the loading line and shows the skeleton', () => {
    render(withI18n(<LoadingState testId="x" />));

    expect(screen.getByRole('status')).toHaveTextContent('Loading…');
    expect(screen.getByTestId('x-skeleton').children).toHaveLength(6);
    expect(screen.getByTestId('x-loading')).toHaveAttribute('aria-busy', 'true');
  });

  it('uses a custom label and can hide the skeleton', () => {
    render(withI18n(<LoadingState testId="x" label="Loading XFlickr…" skeleton={false} />));

    expect(screen.getByRole('status')).toHaveTextContent('Loading XFlickr…');
    expect(screen.queryByTestId('x-skeleton')).toBeNull();
  });

  it('is translated to Vietnamese', () => {
    render(withI18n(<LoadingState testId="x" />, 'vi'));

    expect(screen.getByRole('status')).toHaveTextContent('Đang tải…');
  });
});

describe('EmptyState', () => {
  it('shows default copy and an optional action', () => {
    render(withI18n(<EmptyState testId="x" action={<button type="button">Go</button>} />));

    expect(screen.getByTestId('x-empty')).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Nothing here yet' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Go' })).toBeInTheDocument();
  });

  it('takes custom copy', () => {
    render(withI18n(<EmptyState testId="x" icon="tray" title="Empty tray" text="Add photos" />));

    expect(screen.getByRole('heading', { name: 'Empty tray' })).toBeInTheDocument();
    expect(screen.getByText('Add photos')).toBeInTheDocument();
  });
});

describe('ErrorState', () => {
  it('shows an alert with Retry that calls back', () => {
    const onRetry = vi.fn();
    render(withI18n(<ErrorState testId="x" onRetry={onRetry} withSkeleton />));

    expect(screen.getByRole('alert')).toHaveTextContent('Something went wrong');
    fireEvent.click(screen.getByTestId('x-error-retry'));
    expect(onRetry).toHaveBeenCalledOnce();
  });

  it('omits Retry without a handler and omits empty text', () => {
    render(withI18n(<ErrorState testId="x" title="Failed" text="" />));

    expect(screen.queryByTestId('x-error-retry')).toBeNull();
    expect(screen.getByRole('alert').querySelectorAll('p')).toHaveLength(0);
  });
});

describe('ForbiddenState and NotFoundState', () => {
  it('renders the 403 page with a Go to home button', () => {
    const onHome = vi.fn();
    render(withI18n(<ForbiddenState testId="f" ctaTestId="f-cta" onHome={onHome} />));

    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent(
      'You do not have access to this page',
    );
    fireEvent.click(screen.getByTestId('f-cta'));
    expect(onHome).toHaveBeenCalledOnce();
  });

  it('renders the 404 page with Home and Back', () => {
    const onHome = vi.fn();
    const onBack = vi.fn();
    render(withI18n(<NotFoundState testId="n" onHome={onHome} onBack={onBack} />, 'vi'));

    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent(
      'Chúng tôi không tìm thấy trang đó',
    );
    fireEvent.click(screen.getByTestId('n-home'));
    fireEvent.click(screen.getByTestId('n-back'));
    expect(onHome).toHaveBeenCalledOnce();
    expect(onBack).toHaveBeenCalledOnce();
  });
});

describe('PageState and StatePreview', () => {
  const states: [ViewState, string][] = [
    ['normal', 'content'],
    ['loading', 'p-loading'],
    ['empty', 'p-empty'],
    ['error', 'p-error'],
  ];

  it.each(states)('renders the %s state', (state, marker) => {
    render(
      withI18n(
        <PageState state={state} testId="p">
          content
        </PageState>,
      ),
    );

    if (state === 'normal') expect(screen.getByText(marker)).toBeInTheDocument();
    else expect(screen.getByTestId(marker)).toBeInTheDocument();
  });

  it('passes empty copy and the retry handler through', () => {
    const onRetry = vi.fn();
    const { rerender } = render(
      withI18n(
        <PageState state="empty" testId="p" emptyTitle="No photos" emptyText="Crawl first">
          x
        </PageState>,
      ),
    );
    expect(screen.getByRole('heading', { name: 'No photos' })).toBeInTheDocument();

    rerender(
      withI18n(
        <PageState state="error" testId="p" onRetry={onRetry}>
          x
        </PageState>,
      ),
    );
    fireEvent.click(screen.getByTestId('p-error-retry'));
    expect(onRetry).toHaveBeenCalledOnce();
  });

  it('switches between the four preview states', () => {
    const onChange = vi.fn();
    render(withI18n(<StatePreview state="loading" onChange={onChange} />));

    expect(screen.getByTestId('shell-preview-loading')).toHaveAttribute('aria-pressed', 'true');
    expect(screen.getByTestId('shell-preview-empty')).toHaveAttribute('aria-pressed', 'false');
    fireEvent.click(screen.getByTestId('shell-preview-error'));
    expect(onChange).toHaveBeenCalledWith('error');
  });

  it('renders no preview control in a production build', () => {
    vi.stubEnv('DEV', false);
    render(withI18n(<StatePreview state="normal" onChange={vi.fn()} />));

    expect(screen.queryByRole('group')).toBeNull();
    vi.unstubAllEnvs();
  });
});

describe('useLoadingTiming', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('shows the skeleton only after 200 ms and keeps it for at least 400 ms', () => {
    const { result, rerender } = renderHook(({ loading }) => useLoadingTiming(loading), {
      initialProps: { loading: true },
    });

    expect(result.current).toBe(false);
    act(() => {
      vi.advanceTimersByTime(199);
    });
    expect(result.current).toBe(false);
    act(() => {
      vi.advanceTimersByTime(1);
    });
    expect(result.current).toBe(true);

    rerender({ loading: false });
    act(() => {
      vi.advanceTimersByTime(300);
    });
    expect(result.current).toBe(true);
    act(() => {
      vi.advanceTimersByTime(100);
    });
    expect(result.current).toBe(false);
  });

  it('never shows the skeleton when loading ends before 200 ms', () => {
    const { result, rerender } = renderHook(({ loading }) => useLoadingTiming(loading), {
      initialProps: { loading: true },
    });

    act(() => {
      vi.advanceTimersByTime(100);
    });
    rerender({ loading: false });
    act(() => {
      vi.advanceTimersByTime(1000);
    });
    expect(result.current).toBe(false);
  });
});
