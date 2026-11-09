import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { mkdtempSync, mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { test } from 'node:test';
import { fileURLToPath } from 'node:url';

const __dirname = dirname(fileURLToPath(import.meta.url));
const script = join(__dirname, 'check-i18n.mjs');
const webRoot = join(__dirname, '..');

function writeLocaleTree(root, en, vi) {
  for (const locale of ['en', 'vi']) {
    mkdirSync(join(root, locale), { recursive: true });
  }
  for (const [ns, flat] of Object.entries(en)) {
    writeFileSync(join(root, 'en', `${ns}.json`), JSON.stringify(flat));
  }
  for (const [ns, flat] of Object.entries(vi)) {
    writeFileSync(join(root, 'vi', `${ns}.json`), JSON.stringify(flat));
  }
}

test('fails when vi is missing a key present in en', () => {
  const root = mkdtempSync(join(tmpdir(), 'check-i18n-'));
  try {
    writeLocaleTree(
      root,
      { common: { save: 'Save', cancel: 'Cancel' } },
      { common: { save: 'Lưu' } },
    );
    const result = spawnSync(process.execPath, [script, '--fixtures', root], {
      encoding: 'utf8',
      cwd: webRoot,
    });
    assert.notEqual(result.status, 0);
    assert.match(result.stderr, /missing in vi: common\.cancel/);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test('keeps a drive-qualified fixture path absolute', () => {
  const fixturePath = 'C:\\Users\\dev\\AppData\\Local\\Temp\\check-i18n-example';
  const fixtureLocalePath = `${fixturePath}${process.platform === 'win32' ? '\\' : '/'}en`;
  const result = spawnSync(process.execPath, [script, '--fixtures', fixturePath], {
    encoding: 'utf8',
    cwd: webRoot,
  });

  assert.notEqual(result.status, 0);
  assert.ok(
    result.stderr.includes(`scandir '${fixtureLocalePath}'`),
    result.stderr || result.stdout,
  );
});

test('passes the real catalogs', () => {
  const result = spawnSync(process.execPath, [script], {
    encoding: 'utf8',
    cwd: webRoot,
  });
  assert.equal(result.status, 0, result.stderr || result.stdout);
  assert.match(result.stdout, /check-i18n: ok/);
});
