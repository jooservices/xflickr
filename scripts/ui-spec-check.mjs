import { readFile } from 'node:fs/promises';

const pocPath = 'docs/ui/poc/xflickr-poc.html';
const designPath = 'docs/ui/design-system.md';
const tokensPath = 'docs/ui/tokens.md';

const [poc, design, tokens] = await Promise.all([
  readFile(pocPath, 'utf8'),
  readFile(designPath, 'utf8'),
  readFile(tokensPath, 'utf8'),
]);

const rootStart = poc.indexOf(':root{');
const rootEnd = poc.indexOf('*{box-sizing:border-box}', rootStart);
if (rootStart < 0 || rootEnd < 0) {
  throw new Error('Could not locate POC root token blocks in ' + pocPath);
}

const rootCss = poc.slice(rootStart, rootEnd);
const pocTokens = new Set(
  [...rootCss.matchAll(/--([a-z0-9-]+)\s*:/g)].map((match) => match[1]),
);
const tokenContract = tokens.slice(tokens.indexOf('## POC coverage check'));
const missingTokens = [...pocTokens].filter(
  (name) => !tokenContract.includes(name) && !tokenContract.includes('--' + name),
);

const iconStart = poc.indexOf('const ICONS = {');
const iconEnd = poc.indexOf('\n};', iconStart);
if (iconStart < 0 || iconEnd < 0) {
  throw new Error('Could not locate POC icon inventory in ' + pocPath);
}

const pocIcons = [
  ...poc.slice(iconStart, iconEnd).matchAll(/^\s{2}([a-zA-Z][\w]*):/gm),
].map((match) => match[1]);
const iconSection = design.slice(
  design.indexOf('## Iconography'),
  design.indexOf('## Photos, avatars, and localization'),
);
const missingIcons = pocIcons.filter((name) => !iconSection.includes(name));

if (missingTokens.length || missingIcons.length) {
  if (missingTokens.length) {
    console.error('Missing POC tokens in token coverage contract: ' + missingTokens.join(', '));
  }
  if (missingIcons.length) {
    console.error('Missing POC icons in design-system inventory: ' + missingIcons.join(', '));
  }
  process.exitCode = 1;
} else {
  console.log(
    'UI spec check passed: ' + pocTokens.size + ' POC tokens and ' + pocIcons.length + ' icons are documented.',
  );
}
