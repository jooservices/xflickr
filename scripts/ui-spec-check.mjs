import { readFile } from 'node:fs/promises';
import { validateUiSpec } from './ui-spec-check-lib.mjs';

const [poc, design] = await Promise.all([
  readFile('docs/ui/poc/xflickr-poc.html', 'utf8'),
  readFile('docs/ui/design-system.md', 'utf8'),
]);
const result = validateUiSpec(poc, design);

if (result.missingTokens.length || result.missingIcons.length) {
  if (result.missingTokens.length) {
    console.error('Missing POC tokens in design-system theme tables: ' + result.missingTokens.join(', '));
  }
  if (result.missingIcons.length) {
    console.error('Missing POC icons in design-system inventory: ' + result.missingIcons.join(', '));
  }
  process.exitCode = 1;
} else {
  console.log(
    'UI spec check passed: ' + result.pocTokenCount + ' POC tokens and ' + result.pocIcons.length + ' icons are documented in all four theme tables.',
  );
}
