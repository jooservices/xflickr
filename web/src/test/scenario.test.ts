import type { Page } from '@playwright/test';
import { describe, expect, it } from 'vitest';

import { runScenario, type Step } from '../../e2e/support/scenario';

describe('runScenario', () => {
  it('runs steps sequentially and awaits each step', async () => {
    const page = {} as Page;
    const order: string[] = [];
    let releaseFirst!: () => void;
    let releaseSecond!: () => void;
    const first = new Promise<void>((resolve) => { releaseFirst = resolve; });
    const second = new Promise<void>((resolve) => { releaseSecond = resolve; });
    const steps: readonly Step[] = [
      async (currentPage) => {
        expect(currentPage).toBe(page);
        order.push('first started');
        await first;
        order.push('first finished');
      },
      async (currentPage) => {
        expect(currentPage).toBe(page);
        order.push('second started');
        await second;
        order.push('second finished');
      },
    ];
    const result = runScenario(page, steps).then(() => { order.push('scenario finished'); });
    expect(order).toEqual(['first started']);
    releaseFirst();
    await first;
    expect(order).toEqual(['first started', 'first finished', 'second started']);
    releaseSecond();
    await result;
    expect(order).toEqual([
      'first started', 'first finished', 'second started', 'second finished', 'scenario finished',
    ]);
  });

  it('stops and preserves the error when a step fails', async () => {
    const error = new Error('step failed');
    const order: string[] = [];
    const steps: readonly Step[] = [
      async () => { order.push('first'); },
      async () => { order.push('failure'); throw error; },
      async () => { order.push('unreachable'); },
    ];
    await expect(runScenario({} as Page, steps)).rejects.toBe(error);
    expect(order).toEqual(['first', 'failure']);
  });
});
