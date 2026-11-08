import { render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { DataText, I18nProvider, Term, isGlossaryTerm, translate } from './index';
import { i18nInstance } from './init';

function GenreChip({ name }: { name: string }) {
  // DATA values are rendered as-is and never passed through t().
  return <span data-testid="genre">{name}</span>;
}

describe('glossary and data terms', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('keeps glossary terms and genre names unchanged in Vietnamese', async () => {
    await i18nInstance.changeLanguage('vi');

    render(
      <I18nProvider initialLocale="vi">
        <Term>cream pie</Term>
        <DataText>street photography</DataText>
        <GenreChip name="Green Bee" />
        <p>{translate('shell.nav.library')}</p>
      </I18nProvider>,
    );

    expect(screen.getByText('cream pie')).toBeInTheDocument();
    expect(screen.getByText('street photography')).toBeInTheDocument();
    expect(screen.getByTestId('genre')).toHaveTextContent('Green Bee');
    expect(screen.getByText('Thư viện')).toBeInTheDocument();
    expect(isGlossaryTerm('cream pie')).toBe(true);
    expect(isGlossaryTerm('unknown')).toBe(false);
  });

  it('warns in development when a Term is missing from the glossary', () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined);
    render(
      <I18nProvider>
        <Term>not-in-glossary</Term>
      </I18nProvider>,
    );
    expect(warn).toHaveBeenCalled();
  });
});
