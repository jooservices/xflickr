import { SetupWizard } from './SetupWizard';
import { setupSteps } from './steps';
import { SetupDataProvider } from './useSetupData';

export { tileBackground } from './setupModel';

/** Route entry for the setup wizard. */
export function SetupPage() {
  return (
    <SetupDataProvider>
      <SetupWizard registry={setupSteps} />
    </SetupDataProvider>
  );
}
