import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { test } from 'node:test';
import { fileURLToPath } from 'node:url';
import { checkLayers } from './check-e2e-layers.mjs';

const script = join(dirname(fileURLToPath(import.meta.url)), 'check-e2e-layers.mjs');

function tree(files) {
  const root = mkdtempSync(join(tmpdir(), 'check-e2e-layers-'));
  for (const [path, text] of Object.entries(files)) {
    mkdirSync(dirname(join(root, path)), { recursive: true });
    writeFileSync(join(root, path), text);
  }
  return root;
}

const GOOD = {
  'steps/a.ts':
    "import { expect } from '@playwright/test';\nimport { env } from '../support/env';\nexport async function openA(page) { await page.goto('/a'); }\n",
  'suites/a.ts':
    "import { openA } from '../steps/a';\nexport async function showA(page) { await openA(page); }\n",
  'scenarios/a.spec.ts':
    "import { test } from '@playwright/test';\nimport { showA } from '../suites/a';\ntest('a', async ({ page }) => { await showA(page); });\n",
  'support/env.ts': "import { readFileSync } from 'node:fs';\nexport const env = 1;\n",
};

function problemsOf(files) {
  const root = tree(files);
  try {
    return checkLayers(root);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
}

test('accepts scenarios that call suites that call steps', () => {
  assert.deepEqual(problemsOf(GOOD), []);
  assert.deepEqual(problemsOf({ ...GOOD, 'steps/nested/notes.txt': 'notes' }), []);
  assert.deepEqual(problemsOf({}), []);
});

test('a scenario may not import a step or use the page', () => {
  const problems = problemsOf({
    ...GOOD,
    'scenarios/a.spec.ts':
      "import { test } from '@playwright/test';\nimport { openA } from '../steps/a';\ntest('a', async ({ page }) => { await page.goto('/a'); });\n",
  });
  assert.ok(problems.some((line) => line.includes("a scenario may not import '../steps/a'")));
  assert.ok(problems.some((line) => line.includes('may not use the page directly')));
});

test('AC6 a scenario may pass an aliased page to a suite but not use it directly', () => {
  const scenario = (action) =>
    `import { test } from '@playwright/test';\ntest('a', async ({ page: browserPage }) => { ${action} });\n`;
  assert.deepEqual(
    problemsOf({ ...GOOD, 'scenarios/a.spec.ts': scenario('await showA(browserPage);') }),
    [],
  );
  const problems = problemsOf({
    ...GOOD,
    'scenarios/a.spec.ts': scenario("await browserPage.goto('/a');"),
  });
  assert.ok(problems.some((line) => line.includes('may not use the page directly')));
  const bracketAccess = problemsOf({
    ...GOOD,
    'scenarios/a.spec.ts': scenario("await browserPage['goto']('/a');"),
  });
  assert.ok(bracketAccess.some((line) => line.includes('may not use the page directly')));
  const aliasedSuite = problemsOf({
    ...GOOD,
    'suites/a.ts': 'export async function showA({ page: view }) { await view.reload(); }\n',
  });
  assert.ok(aliasedSuite.some((line) => line.includes('may not use the page directly')));
  const destructured = problemsOf({
    ...GOOD,
    'scenarios/a.spec.ts': "test('a', async ({ page: { goto } }) => { await goto('/a'); });\n",
  });
  assert.ok(destructured.some((line) => line.includes('may not use the page directly')));
});

function scenarioWith(body, params = '{ page }') {
  return {
    ...GOOD,
    'scenarios/a.spec.ts': `import { test } from '@playwright/test';\ntest('a', async (${params}) => { ${body} });\n`,
  };
}

const STANDARD = 'a scenario may not use the page directly; call a step';

test('AC1 a scenario may not use the page through an assignment alias', () => {
  const problems = problemsOf(scenarioWith("const p = page; await p.goto('/x');"));
  assert.equal(problems.filter((line) => line.includes(STANDARD)).length, 1);
  const suite = problemsOf({
    ...GOOD,
    'suites/a.ts': 'export async function showA(page) { const v = page; await v.reload(); }\n',
  });
  assert.ok(suite.some((line) => line.includes('a suite may not use the page directly')));
});

test('AC2 a scenario may not read the page from a fixture object', () => {
  const problems = problemsOf(scenarioWith("await fx.page.goto('/x');", 'fx'));
  assert.equal(problems.filter((line) => line.includes(STANDARD)).length, 1);
  const stored = problemsOf(scenarioWith("const p = fx.page; await p.goto('/x');", 'fx'));
  assert.equal(stored.filter((line) => line.includes(STANDARD)).length, 1);
  const passedOn = problemsOf(scenarioWith('await showA(fx.page);', 'fx'));
  assert.deepEqual(passedOn, []);
});

test('AC3 a scenario may not open a page from the fixture context', () => {
  const plain = problemsOf(
    scenarioWith("const q = await context.newPage(); await q.goto('/x');", '{ context }'),
  );
  assert.equal(plain.filter((line) => line.includes(STANDARD)).length, 1);
  const aliased = problemsOf(
    scenarioWith("const q = (await ctx.newPage()); await q.goto('/x');", '{ context: ctx }'),
  );
  assert.equal(aliased.filter((line) => line.includes(STANDARD)).length, 1);
  const viaFixture = problemsOf(
    scenarioWith("const q = await fx.context.newPage(); await q.goto('/x');", 'fx'),
  );
  assert.equal(viaFixture.filter((line) => line.includes(STANDARD)).length, 1);
});

test('AC4 passing the page to a step, or using neither page nor context, is allowed', () => {
  assert.deepEqual(problemsOf(scenarioWith('await showA(page);')), []);
  assert.deepEqual(
    problemsOf(scenarioWith('const n = 1; const o = other(); await run(n, o);', '')),
    [],
  );
  assert.deepEqual(
    problemsOf(scenarioWith('const q = await context.cookies(); await showA(q);', '{ context }')),
    [],
  );
  assert.deepEqual(problemsOf(scenarioWith('const q = await make(); await showA(q);')), []);
});

test('a suite may not skip to the page, import a scenario or hold a test', () => {
  const problems = problemsOf({
    ...GOOD,
    'suites/a.ts':
      "import { x } from '../scenarios/a.spec';\nexport async function showA(page) { await page.getByTestId('x').click(); }\ntest('t', () => {});\n",
  });
  assert.ok(problems.some((line) => line.includes("a suite may not import '../scenarios/a.spec'")));
  assert.ok(
    problems.some((line) => line.includes('suites/a.ts: a suite may not use the page directly')),
  );
  assert.ok(problems.some((line) => line.includes('tests belong to scenarios only')));
});

test('a step may not import a suite and support may not import a layer', () => {
  const problems = problemsOf({
    ...GOOD,
    'steps/a.ts': "import { showA } from '../suites/a';\n",
    'support/env.ts': "import { openA } from '../steps/a';\nexport const env = 1;\n",
  });
  assert.ok(problems.some((line) => line.includes("a step may not import '../suites/a'")));
  assert.ok(problems.some((line) => line.includes("a support may not import '../steps/a'")));
});

test('an unknown directory and a scenario without a test are reported', () => {
  const problems = problemsOf({
    ...GOOD,
    'helpers/x.ts': 'export {};\n',
    'scenarios/empty.spec.ts': "import { test } from '@playwright/test';\n",
  });
  assert.ok(problems.some((line) => line.startsWith('helpers/: unknown directory')));
  assert.ok(problems.some((line) => line.includes('a scenario file must hold a test')));
});

test('the command prints OK for a clean tree and fails with the list for a broken one', () => {
  const clean = tree(GOOD);
  const broken = tree({
    ...GOOD,
    'suites/a.ts': "export async function showA(page) { await page.goto('/a'); }\n",
  });
  try {
    const ok = spawnSync('node', [script, '--root', clean], { encoding: 'utf8' });
    assert.equal(ok.status, 0);
    assert.match(ok.stdout, /check-e2e-layers: OK/);
    const actual = spawnSync('node', [script], { encoding: 'utf8' });
    assert.equal(actual.status, 0);
    const bad = spawnSync('node', [script, '--root', broken], { encoding: 'utf8' });
    assert.equal(bad.status, 1);
    assert.match(bad.stderr, /1 problem\(s\)/);
  } finally {
    rmSync(clean, { recursive: true, force: true });
    rmSync(broken, { recursive: true, force: true });
  }
});

test('AC6 the e2e directory keeps its three layers', () => {
  const real = join(dirname(fileURLToPath(import.meta.url)), '..', 'e2e');
  assert.deepEqual(checkLayers(real), []);
});
