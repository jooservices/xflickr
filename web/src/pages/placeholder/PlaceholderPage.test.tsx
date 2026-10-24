import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { describe, expect, it } from 'vitest';
import { I18nProvider } from '../../shared';
import { PlaceholderPage } from './PlaceholderPage';

function renderPage() {
  return render(
    <MemoryRouter>
      <I18nProvider initialLocale="vi">
        <PlaceholderPage
          scope="shell"
          titleKey="shell.nav.library"
          emptyTitleKey="shell.home.empty.title"
          emptyTextKey="shell.home.empty.text"
        />
      </I18nProvider>
    </MemoryRouter>,
  );
}

describe('PlaceholderPage', () => {
  it('shows the translated title, sets the document title and the placeholder text', () => {
    renderPage();

    expect(screen.getByRole('heading', { level: 1, name: 'Thư viện' })).toBeInTheDocument();
    expect(document.title).toBe('Thư viện · XFlickr');
    expect(screen.getByText('Trang này chưa được xây dựng.')).toBeInTheDocument();
  });

  it('previews the empty state with the given copy', () => {
    renderPage();

    fireEvent.click(screen.getByTestId('shell-preview-empty'));

    expect(screen.getByTestId('shell-empty')).toHaveTextContent('Chưa thu thập gì');
  });

  it('previews loading, and Retry on the error state loads then returns to normal', async () => {
    renderPage();

    fireEvent.click(screen.getByTestId('shell-preview-loading'));
    expect(screen.getByTestId('shell-skeleton')).toBeInTheDocument();

    fireEvent.click(screen.getByTestId('shell-preview-error'));
    fireEvent.click(screen.getByTestId('shell-error-retry'));
    expect(screen.getByTestId('shell-loading')).toBeInTheDocument();
    await waitFor(() => {
      expect(screen.getByText('Trang này chưa được xây dựng.')).toBeInTheDocument();
    });
  });
});
