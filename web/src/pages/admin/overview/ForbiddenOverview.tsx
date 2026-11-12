import type { JSX } from 'react';
import { useNavigate } from 'react-router';
import { ForbiddenState } from '../../../shared';

/** The shared forbidden state inside the overview page markers. */
export function ForbiddenOverview(): JSX.Element {
  const navigate = useNavigate();
  return (
    <div data-testid="admin-page">
      <div data-testid="admin-overview-page">
        <ForbiddenState
          testId="admin-overview-forbidden"
          ctaTestId="admin-overview-forbidden-home"
          onHome={() => void navigate('/')}
        />
      </div>
    </div>
  );
}
