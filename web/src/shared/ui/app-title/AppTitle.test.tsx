import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { AppTitle } from './AppTitle';

describe('AppTitle', () => {
  it('renders the default app title', () => {
    render(<AppTitle />);

    expect(screen.getByRole('heading', { level: 1, name: 'XFlickr' })).toBeInTheDocument();
  });

  it('renders a custom title', () => {
    render(<AppTitle title="Archive" />);

    expect(screen.getByRole('heading', { level: 1, name: 'Archive' })).toBeInTheDocument();
  });

  it('falls back when the title is blank', () => {
    render(<AppTitle title="  " />);

    expect(screen.getByRole('heading', { level: 1, name: 'XFlickr' })).toBeInTheDocument();
  });
});
