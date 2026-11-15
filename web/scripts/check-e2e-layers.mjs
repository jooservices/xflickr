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
import ts from 'typescript';

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

function unwrap(expression) {
  let node = expression;
  while (node && (ts.isAwaitExpression(node) || ts.isParenthesizedExpression(node))) {
    node = node.expression;
  }
  return node;
}

function isFixtureContext(node, contexts) {
  return (
    (ts.isIdentifier(node) && contexts.has(node.text)) ||
    (ts.isPropertyAccessExpression(node) &&
      ts.isIdentifier(node.expression) &&
      node.name.text === 'context')
  );
}

function isAccess(node) {
  return ts.isPropertyAccessExpression(node) || ts.isElementAccessExpression(node);
}

function isFixturePage(node) {
  return (
    ts.isPropertyAccessExpression(node) &&
    ts.isIdentifier(node.expression) &&
    node.name.text === 'page'
  );
}

function isNewPage(node, contexts) {
  return (
    ts.isCallExpression(node) &&
    ts.isPropertyAccessExpression(node.expression) &&
    node.expression.name.text === 'newPage' &&
    isFixtureContext(node.expression.expression, contexts)
  );
}

function holdsPage(initializer, state) {
  const value = unwrap(initializer);
  if (!value) return false;
  if (ts.isIdentifier(value)) return state.names.has(value.text);
  return isFixturePage(value) || isNewPage(value, state.contexts);
}

function recordBinding(node, tree, state) {
  const property = node.propertyName?.getText(tree);
  if (!ts.isIdentifier(node.name)) {
    state.destructuredPage ||= property === 'page';
  } else if (property === 'page') {
    state.names.add(node.name.text);
  } else if (property === 'context') {
    state.contexts.add(node.name.text);
  }
}

function collect(node, tree, state) {
  if (ts.isBindingElement(node)) recordBinding(node, tree, state);
  if (
    ts.isVariableDeclaration(node) &&
    ts.isIdentifier(node.name) &&
    holdsPage(node.initializer, state)
  ) {
    state.names.add(node.name.text);
  }
  ts.forEachChild(node, (child) => collect(child, tree, state));
}

function touchesPage(node, names) {
  if (!isAccess(node)) return false;
  const target = node.expression;
  return (ts.isIdentifier(target) && names.has(target.text)) || isFixturePage(target);
}

function direct(node, names) {
  return (
    touchesPage(node, names) || (ts.forEachChild(node, (child) => direct(child, names)) ?? false)
  );
}

/**
 * True when the code touches the page itself: the `page` fixture, a destructured alias, an assignment alias
 * (`const p = page` or `const p = fx.page`), the fixture object's property used further (`fx.page.goto`; `fx.page`
 * only passed on is allowed) or a page opened from the fixture context (`await context.newPage()`). Passing the
 * page to a step is allowed. Known limit: aliases created through function calls, arrays or objects are not
 * followed; steps carry the page, which keeps scenarios readable.
 */
function usesPageDirectly(source, file) {
  const tree = ts.createSourceFile(file, source, ts.ScriptTarget.Latest, true);
  const state = {
    names: new Set(['page']),
    contexts: new Set(['context']),
    destructuredPage: false,
  };
  collect(tree, tree, state);
  return state.destructuredPage || direct(tree, state.names);
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
      if ((layer === 'scenarios' || layer === 'suites') && usesPageDirectly(source, file)) {
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
