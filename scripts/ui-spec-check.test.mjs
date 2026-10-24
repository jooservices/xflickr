import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';
import {
  COMPONENT_NAMES,
  COMPONENT_STATES,
  missingPatternSections,
  patternSections,
  validateComponentGallery,
  validateComponentSpec,
  validateUiSpec,
} from './ui-spec-check-lib.mjs';

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
  const { missingTokens, missingIcons, missingMediaOverrides } = validateUiSpec(poc, design);
  assert.deepEqual(missingTokens, []);
  assert.deepEqual(missingIcons, []);
  assert.deepEqual(missingMediaOverrides, []);
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
  assert.deepEqual(result.missingMediaOverrides, []);
});

function specWith(themeRows) {
  return (
    themeHeadings.map((heading) => `### ${heading}\n${themeRows}\n`).join('\n') +
    '\n## Iconography\nsearch\n## Photos, avatars, and localization\n'
  );
}

function pocWith(css) {
  return `:root{--bg:#fff}\n${css}\n*{box-sizing:border-box}\nconst ICONS = {\n  search: '<path/>',\n};\n`;
}

test('fails when a POC viewport override of a root token is undocumented', async () => {
  const [poc, design, tokens] = await Promise.all([
    readFile('docs/ui/poc/xflickr-poc.html', 'utf8'),
    readFile('docs/ui/design-system.md', 'utf8'),
    readFile('docs/ui/tokens.md', 'utf8'),
  ]);
  for (const heading of themeHeadings) {
    assert.deepEqual(tokenRow(themeSection(design, heading), 'row-h'), ['row-h', '190px', '190px']);
    assert.deepEqual(tokenRow(themeSection(design, heading), 'row-h@max-720'), ['row-h@max-720', '120px', '120px']);
  }
  assert.match(tokens, /rowHeight: \{default: "190px", "max-720": "120px"\}/);
  assert.match(design, /"rowHeight": \{"default": "190px", "max-720": "120px"\}/);

  const stripped = design.replaceAll('| row-h@max-720 | 120px | 120px |\n', '');
  const { missingMediaOverrides } = validateUiSpec(poc, stripped);
  assert.deepEqual(
    missingMediaOverrides,
    themeHeadings.map((heading) => `${heading}: row-h@max-720 = 120px`),
  );
});

test('records min-width and max-width root overrides and ignores component tokens', () => {
  const poc = pocWith('@media (min-width:900px){:root{--radius:18px}}\n@media (max-width:720px){.hero .jgrid{--row-h:110px}}');
  const missing = validateUiSpec(poc, specWith('| bg | #fff | #fff |'));
  assert.deepEqual(
    missing.missingMediaOverrides,
    themeHeadings.map((heading) => `${heading}: radius@min-900 = 18px`),
  );
  const documented = validateUiSpec(poc, specWith('| bg | #fff | #fff |\n| radius@min-900 | 18px | 18px |'));
  assert.deepEqual(documented.missingMediaOverrides, []);
  assert.equal(documented.mediaOverrideCount, 1);
});

test('requires the POC value of a prefers-color-scheme root override', () => {
  const dark = pocWith('@media (prefers-color-scheme: dark){ :root:not([data-theme="light"]){ --bg:#111 } }');
  const wrong = validateUiSpec(dark, specWith('| bg | #fff | #fff |'));
  assert.deepEqual(wrong.missingMediaOverrides, ['USER dark gallery: bg @prefers-color-scheme dark = #111']);
  const corrected = specWith('| bg | #fff | #fff |').replace(
    '### USER dark gallery\n| bg | #fff | #fff |',
    '### USER dark gallery\n| bg | #222 (corrected) | #111 |',
  );
  assert.deepEqual(validateUiSpec(dark, corrected).missingMediaOverrides, []);

  const adminLight = pocWith(
    '@media (prefers-color-scheme: light){ :root[data-role="admin"]:not([data-theme="dark"]){ --accent:#0c7da3 } }',
  );
  assert.deepEqual(validateUiSpec(adminLight, specWith('| accent | #006482 (corrected) | #006482 |')).missingMediaOverrides, [
    'ADMIN light control room: accent @prefers-color-scheme light = #0c7da3',
  ]);
  const adminDark = pocWith('@media (prefers-color-scheme: dark){:root[data-role="admin"]{--bg:#010101}}');
  assert.deepEqual(validateUiSpec(adminDark, specWith('| bg | #fff | #fff |')).missingMediaOverrides, [
    'ADMIN dark control room: bg @prefers-color-scheme dark = #010101',
  ]);
  const userLight = pocWith('@media (prefers-color-scheme: light){:root{--ink:#123456}}');
  assert.deepEqual(validateUiSpec(userLight, specWith('| ink | #000 | #000 |')).missingMediaOverrides, [
    'USER light gallery: ink @prefers-color-scheme light = #123456',
  ]);
});

test('documents a combined media query with its query slug', () => {
  const poc = pocWith('@media (max-width:720px) and (orientation: landscape){:root{--row-h:100px}}');
  const missing = validateUiSpec(poc, specWith('| row-h | 190px | 190px |'));
  assert.deepEqual(
    missing.missingMediaOverrides,
    themeHeadings.map((heading) => `${heading}: row-h@max-width-720px-and-orientation-landscape = 100px`),
  );
  const documented = validateUiSpec(
    poc,
    specWith('| row-h | 190px | 190px |\n| row-h@max-width-720px-and-orientation-landscape | 100px | 100px |'),
  );
  assert.deepEqual(documented.missingMediaOverrides, []);
});

test('rejects an unclosed POC media query', () => {
  const design = specWith('| bg | #fff |');
  assert.throws(() => validateUiSpec(pocWith('@media (max-width:720px){:root{--row-h:120px}'), design), /Unbalanced brace/);
  assert.throws(() => validateUiSpec(pocWith('@media (max-width:720px){:root}'), design), /Unbalanced brace/);
  const noBlock = `:root{--bg:#fff}\n*{box-sizing:border-box}\nconst ICONS = {\n  search: '<path/>',\n};\n@media (max-width:720px)`;
  assert.throws(() => validateUiSpec(noBlock, design), /Unbalanced brace/);
});

function componentSection(name, states = COMPONENT_STATES) {
  return `## ${name}

Slice: \`web/src/shared/ui/${name}\`.

### Props

| Prop | Type | Default | Description |
| --- | --- | --- | --- |
| label | string | — | Visible name |

### Variants

| Variant | Meaning |
| --- | --- |
| neutral | Resting treatment |

### States

| State | Appearance | Behaviour |
| --- | --- | --- |
${states.map((state) => `| ${state} | Shown | Ready |`).join('\n')}

### Keyboard

- Tab moves focus.

### ARIA

- Native control.
`;
}

function fullComponentSpec(replaceName, replacement) {
  return (
    '# Component library\n\n' +
    COMPONENT_NAMES.map((name) => (name === replaceName ? replacement : componentSection(name))).join('\n') +
    '\n'
  );
}

function galleryFrame(role, theme, components) {
  const body = components
    .map(
      ([name, states]) =>
        `<article data-component="${name}">${states
          .map((state) => `<div data-state="${state}"></div>`)
          .join('')}</article>`,
    )
    .join('');
  return `<section data-theme="${theme}" data-role="${role}">${body}</section>`;
}

function fullGallery() {
  return ['user', 'admin']
    .flatMap((role) => ['light', 'dark'].map((theme) => galleryFrame(role, theme, COMPONENT_NAMES.map((name) => [name, COMPONENT_STATES]))))
    .join('');
}

test('accepts a catalogue where every slice has a state table and gallery cell', () => {
  assert.deepEqual(validateComponentSpec(fullComponentSpec()).missing, []);
  assert.deepEqual(validateComponentGallery(fullGallery()).missing, []);
});

test('requires the slice path, subsections, and a real state table', () => {
  const missingSection = validateComponentSpec(fullComponentSpec('chip', '')).missing;
  assert.ok(missingSection.includes('chip: section'));
  assert.equal(missingSection.some((item) => item.startsWith('chip:')), true);
  assert.equal(missingSection.includes('button: section'), false);

  const missingSlice = validateComponentSpec(
    fullComponentSpec('chip', componentSection('chip').replace('web/src/shared/ui/chip', 'web/src/shared/ui/other')),
  ).missing;
  assert.deepEqual(missingSlice, ['chip: slice']);

  const missingStates = validateComponentSpec(
    fullComponentSpec('button', componentSection('button').replace(/### States[\s\S]*?### Keyboard/, '### Keyboard')),
  ).missing;
  assert.ok(missingStates.includes('button: States'));
  assert.equal(missingStates.includes('button: state default'), false);

  const emptyKeyboard = validateComponentSpec(
    fullComponentSpec('button', componentSection('button').replace('- Tab moves focus.\n\n', '\n')),
  ).missing;
  assert.deepEqual(emptyKeyboard, ['button: Keyboard text']);

  const emptyProps = validateComponentSpec(
    fullComponentSpec(
      'button',
      componentSection('button').replace('| label | string | — | Visible name |\n', ''),
    ),
  ).missing;
  assert.deepEqual(emptyProps, ['button: Props table']);
});

test('counts only the first state table and ignores headings that are not the slice', () => {
  const prose = componentSection(
    'button',
    COMPONENT_STATES.filter((state) => state !== 'hover'),
  ).replace(
    '| error | Shown | Ready |\n',
    '| error | Shown | Ready |\n\nHover is mentioned in prose.\n\n| State | Appearance | Behaviour |\n| :---: | :---: | :---: |\n| hover | Late | Late |\n',
  );
  const missing = validateComponentSpec(fullComponentSpec('button', prose)).missing;
  assert.deepEqual(missing, ['button: state hover']);

  const headerIsNotAState = componentSection('button', COMPONENT_STATES.filter((state) => state !== 'default')).replace(
    '| State | Appearance | Behaviour |\n| --- | --- | --- |\n',
    '| default | Appearance | Behaviour |\n| --- | --- | --- |\n|\n',
  );
  assert.ok(validateComponentSpec(fullComponentSpec('button', headerIsNotAState)).missing.includes('button: state default'));

  const renamed = validateComponentSpec(fullComponentSpec().replace('## button\n', '## button extra\n')).missing;
  assert.ok(renamed.includes('button: section'));

  const laterWins = validateComponentSpec(`${fullComponentSpec()}\n## button\n\nIncomplete.\n`).missing;
  assert.ok(laterWins.includes('button: slice'));
  assert.ok(laterWins.includes('button: States'));
  assert.equal(laterWins.includes('chip: section'), false);
});

test('reads gallery frames, forced states, comments, and void tags', () => {
  const swapped = validateComponentGallery(
    '<section data-theme="dark" DATA-ROLE="admin"><article DATA-COMPONENT="toast" data-state="default"></article></section>',
  ).missing;
  assert.equal(swapped.includes('admin/dark: frame'), false);
  assert.equal(swapped.includes('admin/dark: toast default'), false);
  assert.ok(swapped.includes('admin/dark: toast hover'));
  assert.ok(swapped.includes('user/light: frame'));

  const commented = validateComponentGallery(
    '<!-- <div data-role="user" data-theme="light"><div data-component="button" data-state="hover"></div></div> -->\n' +
      '<div data-role="user" data-theme="light"><div data-component="button" data-state="default"></div></div>',
  ).missing;
  assert.equal(commented.includes('user/light: button default'), false);
  assert.ok(commented.includes('user/light: button hover'));

  const voids = validateComponentGallery(
    '<div data-role=user data-theme=light hidden><div data-component="text-field"><input data-state="error"><div data-state="focus"></div></div></div>',
  ).missing;
  assert.equal(voids.includes('user/light: text-field error'), false);
  assert.equal(voids.includes('user/light: text-field focus'), false);

  const selfClosed = validateComponentGallery(
    '<div data-role="user" data-theme="light"></ghost><span data-component="badge" data-state="loading" /></div>',
  ).missing;
  assert.equal(selfClosed.includes('user/light: badge loading'), false);

  const union = validateComponentGallery(
    '<div data-role="admin" data-theme="light"><div data-component="tree" data-state="default"></div><div data-component="tree"><i data-state="error"></i></div></div>' +
      '<section data-role="admin" data-theme="light"><div data-component="tree" data-state="active"></div></section>',
  ).missing;
  assert.equal(union.includes('admin/light: tree default'), false);
  assert.equal(union.includes('admin/light: tree error'), false);
  assert.equal(union.includes('admin/light: tree active'), false);
  assert.ok(union.includes('admin/light: tree hover'));

  const loose = validateComponentGallery(
    '<div data-component="button" data-state="default"></div><div data-role="user"><div data-state="hover"></div></div><div data-role="user" data-theme="dark"><div data-component="card"></div></div>',
  ).missing;
  assert.ok(loose.includes('user/light: frame'));
  assert.ok(loose.includes('user/dark: card'));
  assert.equal(loose.includes('user/dark: card default'), false);
});

test('reads a grouped :root selector and ignores a :root suffix', () => {
  const grouped = pocWith('@media (max-width:720px){button, :root{--row-h:120px}}');
  assert.deepEqual(
    validateUiSpec(grouped, specWith('| bg | #fff | #fff |')).missingMediaOverrides,
    themeHeadings.map((heading) => `${heading}: row-h@max-720 = 120px`),
  );
  const suffix = pocWith('@media (max-width:720px){div:root{--row-h:120px}}');
  const ignored = validateUiSpec(suffix, specWith('| bg | #fff | #fff |'));
  assert.deepEqual(ignored.missingMediaOverrides, []);
  assert.equal(ignored.mediaOverrideCount, 0);
});

function componentBody(markdown, name) {
  const marker = `\n## ${name}\n`;
  const start = markdown.indexOf(marker);
  assert.ok(start >= 0, name);
  const from = start + marker.length;
  const next = markdown.indexOf('\n## ', from);
  return markdown.slice(from, next < 0 ? undefined : next);
}

test('documents every shared component and shows it in every theme', async () => {
  const [spec, gallery] = await Promise.all([
    readFile('docs/ui/components.md', 'utf8'),
    readFile('docs/ui/mockups/components.html', 'utf8'),
  ]);
  assert.deepEqual(validateComponentSpec(spec).missing, []);
  assert.deepEqual(validateComponentGallery(gallery).missing, []);
  assert.deepEqual(COMPONENT_NAMES.length, 33);

  const phrases = {
    button: ['| primary |', '| danger |', '| ghost |', '| sm |'],
    chip: ['aria-pressed'],
    pill: ['| ok |', '| warn |', '| bad |', '| info |'],
    badge: ['mono'],
    tag: ['search'],
    'text-field': ['aria-invalid'],
    select: ['aria-invalid'],
    switch: ['switch'],
    checkbox: ['aria-invalid'],
    table: ['caption'],
    card: ['interactive'],
    panel: ['header'],
    'stat-strip': ['tabular'],
    banner: ['| info |', '| warn |', '| bad |'],
    'progress-bar': ['progressbar'],
    ring: ['aria-label'],
    sparkline: ['polyline'],
    chart: ['line', 'area', 'bar', 'legend', 'tooltip'],
    'photo-grid': ['justified'],
    'contact-card': ['avatar'],
    'album-card': ['stack'],
    lightbox: ['Escape'],
    modal: ['dialog'],
    drawer: ['dialog'],
    toast: ['polite'],
    tabs: ['subtab'],
    breadcrumb: ['Breadcrumb'],
    'empty-block': ['call to action'],
    skeleton: ['shimmer'],
    'node-card': ['drain'],
    'resource-bar': ['CPU'],
    tree: ['aria-expanded'],
    'event-feed': ['severity'],
  };
  assert.deepEqual(Object.keys(phrases), COMPONENT_NAMES);
  for (const [name, needles] of Object.entries(phrases)) {
    const body = componentBody(spec, name);
    for (const needle of needles) assert.ok(body.includes(needle), `${name} missing ${needle}`);
    assert.ok(body.includes(`web/src/shared/ui/${name}`), name);
  }

  for (const token of ['#747b88', '#778396', '#7a8798', '#f2766b', '#737f90', '#006482', '#b4431a', '#b8481e']) {
    assert.ok(gallery.includes(token), token);
  }
  for (const role of ['user', 'admin']) {
    for (const theme of ['light', 'dark']) {
      assert.ok(gallery.includes(`data-role="${role}"`), role);
      assert.ok(gallery.includes(`data-theme="${theme}"`), theme);
    }
  }
});

test('finds every pattern, accessibility, and i18n section', async () => {
  const files = Object.fromEntries(
    await Promise.all(Object.keys(patternSections).map(async (file) => [file, await readFile(file, 'utf8')])),
  );
  assert.deepEqual(missingPatternSections(files), []);
});

test('reports a missing pattern section and a missing file', () => {
  const files = { 'docs/ui/patterns.md': '## Loading skeletons\n### Toast rules\n' };
  const missing = missingPatternSections(files, {
    'docs/ui/patterns.md': ['Loading skeletons', 'Toast rules', 'Offline mode'],
    'docs/ui/a11y.md': ['Keyboard map'],
  });
  assert.deepEqual(missing, ['docs/ui/patterns.md: Offline mode', 'docs/ui/a11y.md: Keyboard map']);
});
