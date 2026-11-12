import type { ApiKey, FlickrAccount } from '../../shared/api/gen/models';
import { describe, expect, it } from 'vitest';
import { resolveEntry } from './resolveEntry';

const validKey = { id: 1, status: 'valid' } as ApiKey;
const account = { id: 2 } as FlickrAccount;

const data = (keys: ApiKey[] = [], accounts: FlickrAccount[] = []) => ({ keys, accounts });

describe('resolveEntry', () => {
  it.each([
    '/onboarding?connected=1',
    '/onboarding?connect_error=denied',
    '/onboarding?error=denied',
  ])('starts on the connect result page for %s', (url) => {
    expect(resolveEntry(data([validKey]), url)).toBe('connect-result');
  });

  it('resumes at done when an account exists and there is no result', () => {
    expect(resolveEntry(data([validKey], [account]), '/onboarding')).toBe('done');
  });

  it('prioritizes a Flickr result over an existing account', () => {
    expect(resolveEntry(data([validKey], [account]), '/onboarding?connected=1')).toBe(
      'connect-result',
    );
  });

  it('resumes at connect when a valid key exists without an account', () => {
    expect(resolveEntry(data([validKey]), '/onboarding')).toBe('connect');
  });

  it('starts at key when no valid key or account exists', () => {
    expect(resolveEntry(data([{ id: 3, status: 'invalid' } as ApiKey]), '/onboarding')).toBe('key');
  });
});
