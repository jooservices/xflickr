import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';
import { validateUiSpec } from './ui-spec-check-lib.mjs';

test('reports a token removed from theme tables', async () => {
  const [poc, fixture] = await Promise.all([
    readFile('docs/ui/poc/xflickr-poc.html', 'utf8'),
    readFile('scripts/fixtures/design-system-missing-token.md', 'utf8'),
  ]);
  const { missingTokens } = validateUiSpec(poc, fixture);
  assert.ok(missingTokens.includes('USER light gallery: surface'));
  assert.ok(missingTokens.includes('ADMIN light control room: surface'));
});
