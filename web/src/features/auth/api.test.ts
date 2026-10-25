import { describe, expect, it } from 'vitest';
import { validatedReturnTo } from './api';

describe('validatedReturnTo', () => {
  it.each([
    ['/login', '/'],
    ['/login?reason=expired', '/'],
    ['/login/#expired', '/'],
    ['/login/?reason=expired', '/'],
    ['/register?x=1', '/'],
    ['/register/#done', '/'],
  ])('rejects authentication route %s', (value, expected) => {
    expect(validatedReturnTo(value)).toBe(expected);
  });

  it.each(['/photos/42?size=large', '/albums#recent', '/'])('keeps safe path %s', (value) => {
    expect(validatedReturnTo(value)).toBe(value);
  });
});
