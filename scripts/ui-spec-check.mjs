import { readFile } from 'node:fs/promises';
import {
  COMPONENT_NAMES,
  missingPatternSections,
  patternSections,
  validateComponentGallery,
  validateComponentSpec,
  validateUiSpec,
} from './ui-spec-check-lib.mjs';

const [poc, design, components, gallery] = await Promise.all([
  readFile('docs/ui/poc/xflickr-poc.html', 'utf8'),
  readFile('docs/ui/design-system.md', 'utf8'),
  readFile('docs/ui/components.md', 'utf8'),
  readFile('docs/ui/mockups/components.html', 'utf8'),
]);
const result = validateUiSpec(poc, design);
const patternFiles = Object.fromEntries(
  await Promise.all(Object.keys(patternSections).map(async (file) => [file, await readFile(file, 'utf8').catch(() => '')])),
);
const missingSections = missingPatternSections(patternFiles);
const componentSpec = validateComponentSpec(components);
const componentGallery = validateComponentGallery(gallery);
let failed = false;

if (result.missingTokens.length) {
  console.error('Missing POC tokens in design-system theme tables: ' + result.missingTokens.join(', '));
  failed = true;
}
if (result.missingMediaOverrides.length) {
  console.error('Missing POC media-query root token overrides: ' + result.missingMediaOverrides.join(', '));
  failed = true;
}
if (result.missingIcons.length) {
  console.error('Missing POC icons in design-system inventory: ' + result.missingIcons.join(', '));
  failed = true;
}
if (componentSpec.missing.length) {
  for (const gap of componentSpec.missing) console.error('component spec: ' + gap);
  failed = true;
}
if (componentGallery.missing.length) {
  for (const gap of componentGallery.missing) console.error('component gallery: ' + gap);
  failed = true;
}
if (missingSections.length) {
  console.error('Missing pattern document sections: ' + missingSections.join(', '));
  failed = true;
}

if (failed) {
  process.exitCode = 1;
} else {
  console.log(
    'UI spec check passed: ' +
      result.pocTokenCount +
      ' POC tokens, ' +
      result.mediaOverrideCount +
      ' media-query root overrides, ' +
      result.pocIcons.length +
      ' icons are documented, ' +
      COMPONENT_NAMES.length +
      ' shared UI components have a state table and a four-theme gallery, and ' +
      Object.values(patternSections).flat().length +
      ' pattern sections are present.',
  );
}
