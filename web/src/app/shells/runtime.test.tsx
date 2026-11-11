import { renderHook } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { useShellRuntime } from './runtime';

describe('shell runtime', () => {
  it('rejects consumers outside the provider', () => {
    expect(() => renderHook(() => useShellRuntime())).toThrow(
      'useShellRuntime must be used inside ShellRuntimeProvider',
    );
  });
});
