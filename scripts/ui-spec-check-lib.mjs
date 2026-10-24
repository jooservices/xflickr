const themeHeadings = [
  'USER light gallery',
  'USER dark gallery',
  'ADMIN dark control room',
  'ADMIN light control room',
];

export function validateUiSpec(poc, design) {
  const rootStart = poc.indexOf(':root{');
  const rootEnd = poc.indexOf('*{box-sizing:border-box}', rootStart);
  if (rootStart < 0 || rootEnd < 0) {
    throw new Error('Could not locate POC root token blocks');
  }

  const rootCss = poc.slice(rootStart, rootEnd);
  const pocTokens = new Set(
    [...rootCss.matchAll(/--([a-z0-9-]+)\s*:/g)]
      .map((match) => match[1])
      .filter((name) => name !== 'r'),
  );
  const missingTokens = [];
  for (const heading of themeHeadings) {
    const section = themeSection(design, heading);
    const tableTokens = new Set();
    for (const cells of tableRows(section)) {
      for (const cell of [cells[0], cells[2]]) {
        if (/^[a-z][a-z0-9-]*$/.test(cell ?? '')) tableTokens.add(cell);
      }
    }
    for (const token of pocTokens) {
      if (!tableTokens.has(token)) missingTokens.push(`${heading}: ${token}`);
    }
    if (!section) missingTokens.push(`${heading}: theme table`);
  }

  const iconStart = poc.indexOf('const ICONS = {');
  const iconEnd = poc.indexOf('\n};', iconStart);
  if (iconStart < 0 || iconEnd < 0) {
    throw new Error('Could not locate POC icon inventory');
  }
  const pocIcons = [
    ...poc.slice(iconStart, iconEnd).matchAll(/(?:^|,)\s*([a-zA-Z][\w]*)\s*:/gm),
  ].map((match) => match[1]);
  const iconStartInDesign = design.indexOf('## Iconography');
  const iconEndInDesign = design.indexOf('## Photos, avatars, and localization');
  const iconSection = design.slice(iconStartInDesign, iconEndInDesign);
  const missingIcons = pocIcons.filter((name) => !iconSection.includes(name));

  const mediaOverrides = rootMediaOverrides(poc);
  const missingMediaOverrides = undocumentedMediaOverrides(design, mediaOverrides);

  return {
    pocTokenCount: pocTokens.size,
    pocIcons,
    mediaOverrideCount: mediaOverrides.length,
    missingTokens,
    missingIcons,
    missingMediaOverrides,
  };
}

function themeSection(design, heading) {
  const sectionStart = design.indexOf('### ' + heading);
  if (sectionStart < 0) return '';
  const nextHeading = design.indexOf('\n### ', sectionStart + 1);
  return design.slice(sectionStart, nextHeading < 0 ? undefined : nextHeading);
}

function tableRows(section) {
  const rows = [];
  for (const row of section.matchAll(/^\|([^\n]+)\|\s*$/gm)) {
    rows.push(row[1].split('|').map((cell) => cell.trim()));
  }
  return rows;
}

function matchingBrace(source, openIndex) {
  let depth = 0;
  for (let index = openIndex; index < source.length; index += 1) {
    const char = source[index];
    if (char === '{') depth += 1;
    else if (char === '}') {
      depth -= 1;
      if (depth === 0) return index;
    }
  }
  throw new Error('Unbalanced brace in POC media query');
}

function rootMediaOverrides(poc) {
  const overrides = [];
  const mediaRe = /@media\b/g;
  for (let match = mediaRe.exec(poc); match; match = mediaRe.exec(poc)) {
    const open = poc.indexOf('{', match.index);
    if (open < 0) throw new Error('Unbalanced brace in POC media query');
    const close = matchingBrace(poc, open);
    const query = poc.slice(match.index + '@media'.length, open).trim();
    const body = poc.slice(open + 1, close);
    for (const rule of rootRules(body)) {
      for (const declaration of rule.declarations) {
        overrides.push({ query, selector: rule.selector, ...declaration });
      }
    }
    mediaRe.lastIndex = close + 1;
  }
  return overrides;
}

function rootRules(css) {
  const rules = [];
  let from = 0;
  while (from < css.length) {
    const rootAt = css.indexOf(':root', from);
    if (rootAt < 0) break;
    let selectorStart = rootAt;
    while (selectorStart > 0 && css[selectorStart - 1] !== '{' && css[selectorStart - 1] !== '}') {
      selectorStart -= 1;
    }
    const open = css.indexOf('{', rootAt);
    if (open < 0) throw new Error('Unbalanced brace in POC media query');
    const close = matchingBrace(css, open);
    const selector = css.slice(selectorStart, open).trim();
    if (targetsRoot(selector)) {
      const declarations = [];
      for (const prop of css.slice(open + 1, close).matchAll(/--([a-z0-9-]+)\s*:\s*([^;]+)/g)) {
        declarations.push({ token: prop[1], value: prop[2].trim() });
      }
      rules.push({ selector, declarations });
    }
    from = close + 1;
  }
  return rules;
}

function targetsRoot(selector) {
  return selector.split(',').some((part) => part.trim().startsWith(':root'));
}

function undocumentedMediaOverrides(design, overrides) {
  const missing = [];
  for (const override of overrides) {
    const widths = [...override.query.matchAll(/(max|min)-width\s*:\s*(\d+)px/g)];
    const schemes = [...override.query.matchAll(/prefers-color-scheme\s*:\s*(dark|light)/g)];
    if (widths.length === 1 && queryRemainder(override.query, /(max|min)-width\s*:\s*\d+px/g) === '') {
      const key = `${widths[0][1]}-${widths[0][2]}`;
      for (const heading of themeHeadings) {
        if (!hasViewportRow(themeSection(design, heading), override.token, key, override.value)) {
          missing.push(`${heading}: ${override.token}@${key} = ${override.value}`);
        }
      }
      continue;
    }
    if (schemes.length === 1 && queryRemainder(override.query, /prefers-color-scheme\s*:\s*(?:dark|light)/g) === '') {
      const heading = colorSchemeTheme(schemes[0][1], override.selector);
      if (!hasPocValue(themeSection(design, heading), override.token, override.value)) {
        missing.push(`${heading}: ${override.token} @prefers-color-scheme ${schemes[0][1]} = ${override.value}`);
      }
      continue;
    }
    const slug = override.query.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '');
    for (const heading of themeHeadings) {
      if (!hasViewportRow(themeSection(design, heading), override.token, slug, override.value)) {
        missing.push(`${heading}: ${override.token}@${slug} = ${override.value}`);
      }
    }
  }
  return missing;
}

function queryRemainder(query, feature) {
  return query
    .replace(feature, '')
    .replace(/\band\b|\bnot\b|\bonly\b/g, '')
    .replace(/[()\s,]+/g, '');
}

function colorSchemeTheme(scheme, selector) {
  const admin = /data-role\s*=\s*["']admin["']/.test(selector);
  if (scheme === 'dark') return admin ? 'ADMIN dark control room' : 'USER dark gallery';
  return admin ? 'ADMIN light control room' : 'USER light gallery';
}

function hasViewportRow(section, token, key, value) {
  return tableRows(section).some((cells) => cells[0] === `${token}@${key}` && cells[1] === value && cells[2] === value);
}

function hasPocValue(section, token, value) {
  return tableRows(section).some((cells) => cells[0] === token && cells[2] === value);
}

// Feature-sliced slices under web/src/shared/ui. Names are the directory under
// that layer. Order matches the component catalogue in docs/ui/components.md.
export const COMPONENT_NAMES = [
  'button',
  'chip',
  'pill',
  'badge',
  'tag',
  'text-field',
  'select',
  'switch',
  'checkbox',
  'table',
  'card',
  'panel',
  'stat-strip',
  'banner',
  'progress-bar',
  'ring',
  'sparkline',
  'chart',
  'photo-grid',
  'contact-card',
  'album-card',
  'lightbox',
  'modal',
  'drawer',
  'toast',
  'tabs',
  'breadcrumb',
  'empty-block',
  'skeleton',
  'node-card',
  'resource-bar',
  'tree',
  'event-feed',
];

export const COMPONENT_STATES = ['default', 'hover', 'focus', 'active', 'disabled', 'loading', 'error'];

export const GALLERY_ROLES = ['user', 'admin'];
export const GALLERY_THEMES = ['light', 'dark'];

const COMPONENT_PARTS = ['Props', 'Variants', 'States', 'Keyboard', 'ARIA'];
const TABLE_PARTS = new Set(['Props', 'Variants', 'States']);

const VOID_TAGS = new Set([
  'area',
  'base',
  'br',
  'col',
  'embed',
  'hr',
  'img',
  'input',
  'link',
  'meta',
  'path',
  'circle',
  'ellipse',
  'line',
  'polyline',
  'polygon',
  'rect',
  'use',
  'stop',
  'source',
  'track',
  'wbr',
]);

export function validateComponentSpec(markdown) {
  const sections = headingSections(markdown, 2);
  const missing = [];
  for (const name of COMPONENT_NAMES) {
    const body = sections.get(name);
    if (body === undefined) {
      missing.push(`${name}: section`);
      continue;
    }
    if (!body.includes(`web/src/shared/ui/${name}`)) missing.push(`${name}: slice`);
    const parts = headingSections(body, 3);
    for (const heading of COMPONENT_PARTS) {
      const part = parts.get(heading);
      if (part === undefined) {
        missing.push(`${name}: ${heading}`);
        continue;
      }
      if (TABLE_PARTS.has(heading) && tableDataRows(part).length === 0) {
        missing.push(`${name}: ${heading} table`);
      } else if (!TABLE_PARTS.has(heading) && part.trim() === '') {
        missing.push(`${name}: ${heading} text`);
      }
      if (heading === 'States') {
        const found = new Set(tableDataRows(part).map((row) => row[0]));
        for (const state of COMPONENT_STATES) {
          if (!found.has(state)) missing.push(`${name}: state ${state}`);
        }
      }
    }
  }
  return { missing };
}

export function validateComponentGallery(html) {
  const frames = indexGallery(html);
  const missing = [];
  for (const role of GALLERY_ROLES) {
    for (const theme of GALLERY_THEMES) {
      const key = `${role}/${theme}`;
      const components = frames.get(key);
      if (!components) {
        missing.push(`${key}: frame`);
        continue;
      }
      for (const name of COMPONENT_NAMES) {
        const states = components.get(name);
        // A framed component with no data-state is one gap. Listing every state
        // would hide that the specimen was never marked.
        if (!states || states.size === 0) {
          missing.push(`${key}: ${name}`);
          continue;
        }
        for (const state of COMPONENT_STATES) {
          if (!states.has(state)) missing.push(`${key}: ${name} ${state}`);
        }
      }
    }
  }
  return { missing };
}

function headingSections(markdown, level) {
  const map = new Map();
  const marks = [...markdown.matchAll(new RegExp(`^#{${level}} (?!#)(.*)\\n`, 'gm'))];
  for (let index = 0; index < marks.length; index += 1) {
    const title = marks[index][1].trim();
    const start = marks[index].index + marks[index][0].length;
    const end = index + 1 < marks.length ? marks[index + 1].index : markdown.length;
    map.set(title, markdown.slice(start, end));
  }
  return map;
}

function tableDataRows(section) {
  const rows = [];
  let seen = false;
  for (const line of section.split('\n')) {
    if (!line.startsWith('|')) {
      if (seen) break;
      continue;
    }
    seen = true;
    const cells = line
      .split('|')
      .slice(1, -1)
      .map((cell) => cell.trim());
    if (cells.length === 0 || cells.every((cell) => /^:?-{3,}:?$/.test(cell))) continue;
    rows.push(cells);
  }
  return rows.slice(1);
}

function indexGallery(html) {
  const frames = new Map();
  const stack = [];
  const tagRe = /<!--[\s\S]*?-->|<\/([a-zA-Z0-9]+)\s*>|<([a-zA-Z0-9]+)([^>]*?)(\/?)>/g;
  for (let match = tagRe.exec(html); match; match = tagRe.exec(html)) {
    if (match[0].startsWith('<!--')) continue;
    if (match[1]) {
      const name = match[1].toLowerCase();
      for (let index = stack.length - 1; index >= 0; index -= 1) {
        if (stack[index].tag === name) {
          stack.length = index;
          break;
        }
      }
      continue;
    }
    const tag = match[2].toLowerCase();
    const node = { tag, attrs: parseAttrs(match[3]) };
    const selfClosing = match[4] === '/' || VOID_TAGS.has(tag);
    if (!selfClosing) stack.push(node);
    recordGalleryNode(frames, selfClosing ? [...stack, node] : stack);
  }
  return frames;
}

function recordGalleryNode(frames, stack) {
  let frameKey = '';
  let component = '';
  const states = [];
  for (const node of stack) {
    const role = node.attrs['data-role'];
    const theme = node.attrs['data-theme'];
    if (role && theme) frameKey = `${role}/${theme}`;
    if (node.attrs['data-component']) component = node.attrs['data-component'];
    if (node.attrs['data-state']) states.push(node.attrs['data-state']);
  }
  if (!frameKey || !component) return;
  if (!frames.has(frameKey)) frames.set(frameKey, new Map());
  const components = frames.get(frameKey);
  if (!components.has(component)) components.set(component, new Set());
  const found = components.get(component);
  for (const state of states) found.add(state);
}

function parseAttrs(raw) {
  const attrs = {};
  for (const match of raw.matchAll(/([^\s=]+)(?:\s*=\s*(?:"([^"]*)"|'([^']*)'|(\S+)))?/g)) {
    attrs[match[1].toLowerCase()] = match[2] ?? match[3] ?? match[4] ?? '';
  }
  return attrs;
}
