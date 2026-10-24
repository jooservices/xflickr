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

function channel(value) {
  const srgb = value / 255;
  return srgb <= 0.03928 ? srgb / 12.92 : ((srgb + 0.055) / 1.055) ** 2.4;
}

function contrast(foreground, background) {
  const luminance = (hex) => {
    const rgb = Number.parseInt(hex.slice(1), 16);
    return (
      0.2126 * channel((rgb >> 16) & 255) +
      0.7152 * channel((rgb >> 8) & 255) +
      0.0722 * channel(rgb & 255)
    );
  };
  const lighter = Math.max(luminance(foreground), luminance(background));
  const darker = Math.min(luminance(foreground), luminance(background));
  return (lighter + 0.05) / (darker + 0.05);
}

function round2(value) {
  return Math.round(value * 100) / 100;
}

function themeTokens(section) {
  const tokens = {};
  let inTable = false;
  for (const line of section.split('\n')) {
    if (line.startsWith('| Token |')) {
      inTable = true;
      continue;
    }
    if (line.startsWith('| Foreground |')) break;
    if (!inTable || !line.startsWith('|')) continue;
    const cells = line.split('|').slice(1, -1).map((cell) => cell.trim());
    const value = cells[1]?.replace(' (corrected)', '');
    if (value?.startsWith('#')) tokens[cells[0]] = value;
  }
  return tokens;
}

function contrastRows(section) {
  const rows = [];
  let inTable = false;
  for (const line of section.split('\n')) {
    if (line.startsWith('| Foreground |')) {
      inTable = true;
      continue;
    }
    if (!inTable) continue;
    if (!line.startsWith('|')) break;
    const cells = line.split('|').slice(1, -1).map((cell) => cell.trim());
    if (!/^[a-z0-9]/.test(cells[0])) continue;
    rows.push({ name: cells[0].split(' ')[0], ratios: cells.slice(1, 5).map(Number) });
  }
  return rows;
}

test('records WCAG contrast for every text and graphic color', async () => {
  const design = await readFile('docs/ui/design-system.md', 'utf8');
  const surfaces = ['bg', 'surface', 'sunken', 'raise'];
  const textColors = new Set(['ink', 'muted', 'accent', 'ok', 'warn', 'bad', 'info']);
  const graphicColors = new Set(['fav', 's1', 's2', 's3', 'line2']);
  for (const heading of themeHeadings) {
    const section = themeSection(design, heading);
    const tokens = themeTokens(section);
    const rows = Object.fromEntries(contrastRows(section).map((row) => [row.name, row.ratios]));
    for (const name of [...textColors, ...graphicColors]) {
      const ratios = surfaces.map((surface) => contrast(tokens[name], tokens[surface]));
      assert.deepEqual(rows[name], ratios.map(round2), `${heading} ${name}`);
      const minimum = textColors.has(name) ? 4.5 : 3;
      assert.ok(Math.min(...ratios) >= minimum, `${heading} ${name} ${Math.min(...ratios)}`);
    }
    const onAccent = contrast(tokens['accent-ink'], tokens.accent);
    const documented = section.match(/Text on accent \(`accent-ink` over `accent`\): ([0-9.]+):1/);
    assert.equal(Number(documented[1]), round2(onAccent), heading);
    assert.ok(onAccent >= 4.5, heading);
  }
});

test('rejects a POC that has no token block', () => {
  assert.throws(() => validateUiSpec('<html></html>', ''), /root token blocks/);
  assert.throws(() => validateUiSpec(':root{--bg:#fff}', ''), /root token blocks/);
});

test('rejects a POC that has no icon inventory', () => {
  const poc = ':root{--bg:#fff}*{box-sizing:border-box}';
  assert.throws(() => validateUiSpec(poc, ''), /icon inventory/);
  assert.throws(() => validateUiSpec(`${poc}\nconst ICONS = {`, ''), /icon inventory/);
});

function sectionTable(design, heading) {
  const start = design.indexOf(heading);
  assert.ok(start >= 0, heading);
  const from = start + heading.length;
  const ends = ['\n## ', '\n### ']
    .map((mark) => design.indexOf(mark, from))
    .filter((index) => index >= 0);
  const section = design.slice(start, ends.length === 0 ? undefined : Math.min(...ends));
  const rows = {};
  let inTable = false;
  for (const line of section.split('\n')) {
    if (line.startsWith('| Key |')) {
      inTable = true;
      continue;
    }
    if (!inTable) continue;
    if (!line.startsWith('|')) break;
    const cells = line.split('|').slice(1, -1).map((cell) => cell.trim());
    if (cells[0].includes('---')) continue;
    rows[cells[0]] = cells.slice(1);
  }
  return rows;
}

test('records exact type scale, elevation, and motion tokens', async () => {
  const [design, tokens] = await Promise.all([
    readFile('docs/ui/design-system.md', 'utf8'),
    readFile('docs/ui/tokens.md', 'utf8'),
  ]);
  const typeScale = sectionTable(design, '### Type scale');
  assert.equal(typeScale['body.user'][0], '15px');
  assert.equal(typeScale['body.user'][1], '1.5');
  assert.equal(typeScale['body.admin'][0], '14px');
  assert.equal(typeScale['body.admin'][1], '1.45');
  assert.equal(typeScale['h1.user'][0], 'clamp(24px, 3vw, 32px)');
  assert.equal(typeScale['h1.user'][1], '1.15');
  assert.equal(typeScale['h1.admin'][0], '1.55rem');
  assert.equal(typeScale['h1.admin'][1], '1.15');
  assert.equal(typeScale['h2'][0], '19px');
  assert.equal(typeScale['h2'][1], '1.25');
  assert.equal(typeScale['h3'][0], '15.5px');
  assert.equal(typeScale['h3'][1], '1.3');
  assert.equal(typeScale['h4'][0], '1em');
  assert.equal(typeScale.hero[0], 'clamp(28px, 4vw, 44px)');
  assert.equal(typeScale.hero[1], '1.15');
  assert.equal(typeScale.small[0], '0.86em');
  assert.equal(typeScale.helper[0], '12px');
  assert.equal(typeScale.eyebrow[0], '0.74rem');
  assert.equal(typeScale.eyebrow[2], '600');
  assert.equal(typeScale.eyebrow[3], '0.08em');
  assert.equal(typeScale.mono[0], '0.85em');
  assert.equal(typeScale.panelTitle[0], '0.86rem');
  assert.match(typeScale.numeric[5], /tabular-nums/);

  const elevation = sectionTable(design, '### Elevation');
  assert.equal(elevation.resting[0], '0 1px 3px rgba(0,0,0,.25)');
  assert.equal(elevation.raised[0], '0 2px 8px var(--shade)');
  assert.equal(elevation.hover[0], '0 6px 20px var(--shade)');
  assert.equal(elevation.popover[0], '0 8px 24px var(--shade)');
  assert.equal(elevation.overlay[0], '0 10px 30px var(--shade)');
  assert.equal(elevation.modal[0], '0 20px 50px var(--shade)');
  assert.equal(elevation.hero[0], '0 20px 60px rgba(0,0,0,.5)');
  assert.equal(elevation.drawer[0], '0 0 40px var(--shade)');
  assert.equal(elevation.sheet[0], '0 30px 60px var(--shade)');

  const motion = sectionTable(design, '### Motion');
  assert.deepEqual(motion.feedback.slice(0, 2), ['.15s', 'ease']);
  assert.deepEqual(motion.stateChange.slice(0, 2), ['.2s', 'ease']);
  assert.deepEqual(motion.emphasis.slice(0, 2), ['.35s', 'ease']);
  assert.deepEqual(motion.entrance.slice(0, 2), ['.2s', 'ease']);
  assert.match(motion.entrance[3], /8px/);
  assert.deepEqual(motion.fresh.slice(0, 2), ['1.6s', 'ease-out']);
  assert.deepEqual(motion.loading.slice(0, 3), ['1.2s', 'ease', 'infinite']);
  assert.deepEqual(motion.spin.slice(0, 3), ['.8s', 'linear', 'infinite']);
  assert.deepEqual(motion.progress.slice(0, 2), ['.6s', 'ease']);
  assert.match(design, /motion\.reducedMotion` is `none`/);

  for (const key of [
    'typeScale.h1.user',
    'typeScale.helper',
    'elevation.resting',
    'elevation.hover',
    'elevation.popover',
    'elevation.modal',
    'elevation.hero',
    'motion.feedback',
    'motion.stateChange',
    'motion.entrance',
    'motion.loading',
    'motion.progress',
    'motion.reducedMotion',
  ]) {
    assert.ok(tokens.includes(key), key);
  }
});

test('ignores per-photo ratio data and reports missing themes and icons', () => {
  const poc = `:root{--bg:#fff; --r:1.5}
*{box-sizing:border-box}
const ICONS = {
  search: '<path/>',
  left: '<path/>', right: '<path/>',
  bell: '<path/>',
};
`;
  const design = `### USER light gallery
| bg | #fff | #fff |

### USER dark gallery
| bg | #000 | #000 |

### ADMIN dark control room
| bg | #111 | #111 |

## Iconography
search
## Photos, avatars, and localization
`;
  const result = validateUiSpec(poc, design);
  assert.equal(result.pocTokenCount, 1);
  assert.deepEqual(result.pocIcons, ['search', 'left', 'right', 'bell']);
  assert.deepEqual(result.missingIcons, ['left', 'right', 'bell']);
  assert.ok(result.missingTokens.includes('ADMIN light control room: bg'));
  assert.ok(result.missingTokens.includes('ADMIN light control room: theme table'));
  assert.equal(result.missingTokens.some((item) => item.startsWith('USER light gallery')), false);
});
