import { describe, expect, it } from 'vitest';
import { ApiShapeError, validateBody } from './validate';

describe('additive REST discovery contract', () => {
  const entry = {
    success: true,
    code: 200,
    message: 'Success',
    data: { links: { self: { href: '/api/v1', method: 'GET' } } },
    meta: {},
    errors: null,
    trace_id: '',
  };

  it('validates the new envelope without unwrapping it', () => {
    expect(validateBody('/api/v1', 'GET', 200, entry)).toEqual(entry);
  });

  it('rejects a legacy-shaped success for the new entry point', () => {
    expect(() => validateBody('/api/v1', 'GET', 200, { links: {} })).toThrow(ApiShapeError);
  });

  it('accepts problem responses without changing legacy error decoding', () => {
    const problem = { type: '/problems/unauthenticated', status: 401 };
    expect(validateBody('/api/v1', 'GET', 401, problem)).toEqual(problem);
    const legacy = { code: 'unauthenticated', message: 'Authentication required' };
    expect(validateBody('/me', 'GET', 401, legacy)).toEqual(legacy);
  });
});
