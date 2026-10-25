import { describe, expect, it } from 'vitest';

describe('MSW', () => {
  it('returns the health payload', async () => {
    const response = await fetch('http://localhost/api/health');

    expect(response.ok).toBe(true);
    await expect(response.json()).resolves.toEqual({ status: 'ok', version: 'test' });
  });
});
