import { describe, expect, it } from 'vitest';
import type { ApiKey } from '../../shared/api/gen/models';
import type { KeyUsage } from './api';
import { clock, hintOf, isTerminal, msToReset, percent, pillOf } from './format';

const key = (status: ApiKey['status']): ApiKey => ({
  id: 1,
  label: 'k',
  key_public: '012345••••',
  per_hour_limit: 3600,
  per_second_limit: 4,
  burst: 8,
  status,
  created_at: '2026-10-25T00:00:00Z',
});
const usage = (state: KeyUsage['state']): KeyUsage => ({
  key_id: 1,
  used: 1,
  person_used: 1,
  sizes_used: 0,
  reserve: 100,
  window_resets_at: '2026-10-25T11:00:00Z',
  state,
});

describe('keys format helpers', () => {
  it('picks the pill: refused keys win over the quota state', () => {
    expect(pillOf(key('revoked'), usage('ok'))).toBe('revoked');
    expect(pillOf(key('invalid'), usage('ok'))).toBe('invalid');
    expect(pillOf(key('valid'), usage('reserve'))).toBe('reserve');
    expect(pillOf(key('valid'), undefined)).toBeNull();
  });

  it('picks one hint, most urgent first', () => {
    expect(hintOf('invalid')).toBe('revoked');
    expect(hintOf('exhausted')).toBe('exhausted');
    expect(hintOf('reserve')).toBe('reserve');
    expect(hintOf('ok')).toBeNull();
    expect(isTerminal('revoked')).toBe(true);
    expect(isTerminal('ok')).toBe(false);
  });

  it('clamps percentages', () => {
    expect(percent(50, 100)).toBe(50);
    expect(percent(500, 100)).toBe(100);
    expect(percent(-1, 100)).toBe(0);
    expect(percent(1, 0)).toBe(0);
  });

  it('counts down from the server clock, not the browser clock', () => {
    const left = msToReset('2026-10-25T10:10:00Z', '2026-10-25T10:00:00Z', 1_000, 31_000);
    expect(left).toBe(600_000 - 30_000);
  });

  it('formats clocks', () => {
    expect(clock(65_000)).toBe('01:05');
    expect(clock(3_725_000)).toBe('1:02:05');
    expect(clock(-5)).toBe('00:00');
    expect(clock(null)).toBe('--:--');
    expect(clock(Number.NaN)).toBe('--:--');
  });
});
