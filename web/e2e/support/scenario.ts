import type { Page } from '@playwright/test';

export type Step = (page: Page) => Promise<void>;

export async function runScenario(page: Page, steps: readonly Step[]): Promise<void> {
  for (const step of steps) {
    await step(page);
  }
}
