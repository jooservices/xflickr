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
    const sectionStart = design.indexOf('### ' + heading);
    const nextHeading = design.indexOf('\n### ', sectionStart + 1);
    const section = sectionStart < 0
      ? ''
      : design.slice(sectionStart, nextHeading < 0 ? undefined : nextHeading);
    const tableTokens = new Set();
    for (const row of section.matchAll(/^\|([^\n]+)\|\s*$/gm)) {
      const cells = row[1].split('|').map((cell) => cell.trim());
      for (const cell of [cells[0], cells[2]]) {
        if (/^[a-z][a-z0-9-]*$/.test(cell ?? '')) tableTokens.add(cell);
      }
    }
    for (const token of pocTokens) {
      if (!tableTokens.has(token)) missingTokens.push(`${heading}: ${token}`);
    }
    if (sectionStart < 0) missingTokens.push(`${heading}: theme table`);
  }

  const iconStart = poc.indexOf('const ICONS = {');
  const iconEnd = poc.indexOf('\n};', iconStart);
  if (iconStart < 0 || iconEnd < 0) {
    throw new Error('Could not locate POC icon inventory');
  }
  const pocIcons = [
    ...poc.slice(iconStart, iconEnd).matchAll(/^\s{2}([a-zA-Z][\w]*):/gm),
  ].map((match) => match[1]);
  const iconStartInDesign = design.indexOf('## Iconography');
  const iconEndInDesign = design.indexOf('## Photos, avatars, and localization');
  const iconSection = design.slice(iconStartInDesign, iconEndInDesign);
  const missingIcons = pocIcons.filter((name) => !iconSection.includes(name));

  return { pocTokenCount: pocTokens.size, pocIcons, missingTokens, missingIcons };
}
