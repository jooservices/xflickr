import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';
import { validateUiSpec } from './ui-spec-check-lib.mjs';

const themeHeadings = [
  'USER light gallery',
  'USER dark gallery',
  'ADMIN dark control room',
  'ADMIN light control room',
];

function themeSection(design, heading) {
  const sectionStart = design.indexOf('### ' + heading);
  const nextHeading = design.indexOf('\n### ', sectionStart + 1);
  assert.ok(sectionStart >= 0, heading);
  return design.slice(sectionStart, nextHeading < 0 ? undefined : nextHeading);
}

function tokenRow(section, token) {
  for (const row of section.matchAll(/^\|([^\n]+)\|\s*$/gm)) {
    const cells = row[1].split('|').map((cell) => cell.trim());
    if (cells[0] === token) return cells;
  }
  assert.fail(`missing token row ${token}`);
}

test('reports a token removed from theme tables', async () => {
  const [poc, fixture] = await Promise.all([
    readFile('docs/ui/poc/xflickr-poc.html', 'utf8'),
    readFile('scripts/fixtures/design-system-missing-token.md', 'utf8'),
  ]);
  const { missingTokens } = validateUiSpec(poc, fixture);
  assert.ok(missingTokens.includes('USER light gallery: surface'));
  assert.ok(missingTokens.includes('ADMIN light control room: surface'));
});

test('accepts the design-system token tables and icon inventory', async () => {
  const [poc, design] = await Promise.all([
    readFile('docs/ui/poc/xflickr-poc.html', 'utf8'),
    readFile('docs/ui/design-system.md', 'utf8'),
  ]);
  const { missingTokens, missingIcons } = validateUiSpec(poc, design);
  assert.deepEqual(missingTokens, []);
  assert.deepEqual(missingIcons, []);
});

test('marks every corrected color with its POC value', async () => {
  const design = await readFile('docs/ui/design-system.md', 'utf8');
  const sections = Object.fromEntries(themeHeadings.map((heading) => [heading, themeSection(design, heading)]));
  const corrected = [
    ['USER light gallery', 'line2', '#747b88 (corrected)', '#cfd4de'],
    ['USER light gallery', 'ok', '#187244 (corrected)', '#1d8a52'],
    ['USER light gallery', 'warn', '#875300 (corrected)', '#a86500'],
    ['USER light gallery', 'bad', '#c12824 (corrected)', '#c9302c'],
    ['USER light gallery', 's1', '#1f66b4 (corrected)', '#2a78d6'],
    ['USER light gallery', 's2', '#b4431a (corrected)', '#eb6834'],
    ['USER light gallery', 's3', '#08774f (corrected)', '#1baf7a'],
    ['USER dark gallery', 'line2', '#778396 (corrected)', '#3a4150'],
    ['ADMIN dark control room', 'line2', '#7a8798 (corrected)', '#2e3a4c'],
    ['ADMIN dark control room', 'bad', '#f2766b (corrected)', '#ea5a4e'],
    ['ADMIN light control room', 'line2', '#737f90 (corrected)', '#c9d2de'],
    ['ADMIN light control room', 'accent', '#006482 (corrected)', '#0c7da3'],
    ['ADMIN light control room', 'ok', '#176d42 (corrected)', '#1c8752'],
    ['ADMIN light control room', 'warn', '#815800 (corrected)', '#9c6a00'],
    ['ADMIN light control room', 's1', '#1f66b4 (corrected)', '#2a78d6'],
    ['ADMIN light control room', 's2', '#b8481e (corrected)', '#eb6834'],
    ['ADMIN light control room', 's3', '#08774f (corrected)', '#1baf7a'],
  ];
  for (const [heading, token, required, pocValue] of corrected) {
    assert.deepEqual(tokenRow(sections[heading], token), [token, required, pocValue]);
  }

  const unchanged = [
    ['USER dark gallery', 'ok', '#4fc887'],
    ['USER dark gallery', 'warn', '#f0b04a'],
    ['USER dark gallery', 'bad', '#ff7b72'],
    ['ADMIN light control room', 'bad', '#c5372c'],
    ['ADMIN light control room', 'info', '#5a52c9'],
    ['ADMIN light control room', 'fav', '#c7366f'],
  ];
  for (const [heading, token, value] of unchanged) {
    assert.deepEqual(tokenRow(sections[heading], token), [token, value, value]);
  }
});
