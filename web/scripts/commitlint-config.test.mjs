import assert from 'node:assert/strict';
import { test } from 'node:test';
import { headerPattern } from '../../.github/commitlint.config.mjs';

const parse = (header) => header.match(headerPattern);

test('AC1 a squash header that ends with the pull request number is accepted with every field filled', () => {
  const parts = parse('feat(library): Integrate photo viewer (JOOXF-407) (#182)');
  assert.ok(parts);
  assert.deepEqual(parts.slice(1), ['feat', 'library', 'Integrate photo viewer', 'JOOXF-407']);
});

test('AC2 a header without the suffix is accepted and a malformed header is still refused', () => {
  assert.ok(parse('fix(crawl): Scope sizes scheduling (JOOXF-337)'));
  for (const header of [
    'wrong(crawl): Scope sizes (JOOXF-337)',
    'fix(): Scope sizes (JOOXF-337)',
    'fix(crawl): scope sizes (JOOXF-337)',
    'fix(crawl): Scope sizes. (JOOXF-337)',
    'fix(crawl): Scope sizes',
    'fix(crawl): Scope sizes (JOOXF-337) (#)',
    'fix(crawl): Scope sizes (JOOXF-337) (#12) extra',
  ]) {
    assert.equal(parse(header), null, header);
  }
});

test('AC3 a pull request title without the suffix is accepted', () => {
  assert.ok(parse('ci(workflows): Align CI checks (JOOXF-517)'));
});
