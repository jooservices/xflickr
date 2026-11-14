#!/usr/bin/env node
/**
 * Fails when the E2E code breaks its three layers: a step is one small action, a suite combines steps into a
 * feature, a scenario combines suites into one complete test.
 *
 *   scenarios/  tests only; call suites; never touch the page directly
 *   suites/     call steps; never touch the page directly
 *   steps/      touch the page; hold no tests
 *   support/    infrastructure (environment, data factories, route reading); knows no layer
 *
 * Usage: node scripts/check-e2e-layers.mjs [--root <e2e directory>]
 */
import { existsSync, readdirSync, readFileSync, statSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const ALLOWED_IMPORTS = {
  scenarios: [/^@playwright\/test$/, /^\.\.\/suites\//, /^\.\.\/support\//],
  suites: [/^@playwright\/test$/, /^\.\.\/steps\//, /^\.\.\/support\//],
  steps: [
    /^@playwright\/test$/,
    /^node:/,
    /^\.\//,
    /^\.\.\/support\//,
    /^\.\.\/\.\.\/e2e-mocked\/support$/,
  ],
  support: [/^@playwright\/test$/, /^node:/, /^\.\//],
};
// A direct use of the page: only steps may do it.
const PAGE_USE =
  /\bpage\.(goto|getBy\w+|locator|click|fill|check|evaluate|request|route|reload|waitFor\w*|keyboard|setViewportSize|context|url|on|off|addInitScript|screenshot|press|type)\b/;
const TEST_USE =
  /(?<![.\w])test(\.(describe|use|beforeEach|beforeAll|afterEach|afterAll|skip|only))?\s*\(/;

function filesIn(directory) {
  if (!existsSync(directory)) return [];
  return readdirSync(directory).flatMap((name) => {
    const path = join(directory, name);
    return statSync(path).isDirectory() ? filesIn(path) : /\.(ts|tsx)$/.test(name) ? [path] : [];
  });
}

function importsOf(source) {
  return [...source.matchAll(/(?:^|\n)\s*(?:import|export)\b[^;]*?\bfrom\s+['"]([^'"]+)['"]/g)].map(
    (match) => match[1],
  );
}

/** The layer rules broken under `root` (the e2e directory), as readable lines. */
export function checkLayers(root) {
  const problems = [];
  for (const name of readdirSync(root)) {
    if (statSync(join(root, name)).isDirectory() && !(name in ALLOWED_IMPORTS)) {
      problems.push(
        `${name}/: unknown directory; the layers are scenarios, suites, steps and support`,
      );
    }
  }
  for (const [layer, allowed] of Object.entries(ALLOWED_IMPORTS)) {
    for (const file of filesIn(join(root, layer))) {
      const where = file.slice(root.length + 1);
      const source = readFileSync(file, 'utf8');
      for (const target of importsOf(source)) {
        if (!allowed.some((rule) => rule.test(target))) {
          problems.push(`${where}: a ${layer.replace(/s$/, '')} may not import '${target}'`);
        }
      }
      if ((layer === 'scenarios' || layer === 'suites') && PAGE_USE.test(source)) {
        problems.push(
          `${where}: a ${layer.replace(/s$/, '')} may not use the page directly; call a step`,
        );
      }
      if (layer !== 'scenarios' && TEST_USE.test(source)) {
        problems.push(`${where}: tests belong to scenarios only`);
      }
      if (layer === 'scenarios' && !TEST_USE.test(source)) {
        problems.push(`${where}: a scenario file must hold a test`);
      }
    }
  }
  return problems;
}

const isMain = process.argv[1] === fileURLToPath(import.meta.url);
if (isMain) {
  const args = process.argv.slice(2);
  const rootIndex = args.indexOf('--root');
  const root =
    rootIndex >= 0
      ? args[rootIndex + 1]
      : join(dirname(fileURLToPath(import.meta.url)), '..', 'e2e');
  const problems = checkLayers(root);
  if (problems.length > 0) {
    console.error(`check-e2e-layers: ${problems.length} problem(s)`);
    for (const problem of problems) console.error(`  - ${problem}`);
    process.exit(1);
  }
  console.log('check-e2e-layers: OK');
}
