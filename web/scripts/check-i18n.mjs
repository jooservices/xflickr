#!/usr/bin/env node
/**
 * Fails the build when English and Vietnamese catalogs diverge, a value is
 * empty, a key is unused, or a referenced key is missing from the catalogs.
 *
 * Usage: node scripts/check-i18n.mjs [--fixtures <dir>]
 */
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = dirname(fileURLToPath(import.meta.url));
const webRoot = join(__dirname, '..');

const args = process.argv.slice(2);
const fixtureIdx = args.indexOf('--fixtures');
const fixturesOnly = fixtureIdx >= 0;
const fixturePath = fixturesOnly ? args[fixtureIdx + 1] : '';
const localeRoot = fixturesOnly
  ? fixturePath.startsWith('/')
    ? fixturePath
    : join(webRoot, fixturePath)
  : join(webRoot, 'src/shared/i18n/locales');
const srcRoot = join(webRoot, 'src');

const PLURAL_SUFFIXES = ['_zero', '_one', '_two', '_few', '_many', '_other'];

function readJson(path) {
  return JSON.parse(readFileSync(path, 'utf8'));
}

function listNamespaces(locale) {
  return readdirSync(join(localeRoot, locale))
    .filter((name) => name.endsWith('.json'))
    .map((name) => name.replace(/\.json$/, ''))
    .sort();
}

function loadLocale(locale) {
  const out = {};
  for (const ns of listNamespaces(locale)) {
    const flat = readJson(join(localeRoot, locale, `${ns}.json`));
    for (const [key, value] of Object.entries(flat)) {
      out[`${ns}.${key}`] = value;
    }
  }
  return out;
}

function baseKey(key) {
  for (const suffix of PLURAL_SUFFIXES) {
    if (key.endsWith(suffix)) return key.slice(0, -suffix.length);
  }
  return key;
}

function walkFiles(dir, out = []) {
  for (const name of readdirSync(dir)) {
    const path = join(dir, name);
    const st = statSync(path);
    if (st.isDirectory()) {
      if (name === 'gen' || name === '__boundary_fixtures__' || name === 'locales') continue;
      walkFiles(path, out);
      continue;
    }
    if (/\.(tsx|ts|jsx|js)$/.test(name)) out.push(path);
  }
  return out;
}

function collectUsedKeys(files, namespaces) {
  const used = new Set();
  const dynamicPrefixes = new Set();
  const nsSet = new Set(namespaces);
  const dotted = /['"]([a-z]+(?:\.[a-z0-9_-]+)+)['"]/g;
  // t(`crawl.lane.${lane}`) → keep every catalog key under crawl.lane.*
  const dynamic = /\bt\(\s*`([a-z]+(?:\.[a-z0-9_-]+)*)\./g;
  for (const file of files) {
    const text = readFileSync(file, 'utf8');
    dotted.lastIndex = 0;
    let match;
    while ((match = dotted.exec(text)) !== null) {
      const key = match[1];
      if (nsSet.has(key.split('.', 1)[0])) used.add(key);
    }
    dynamic.lastIndex = 0;
    while ((match = dynamic.exec(text)) !== null) {
      dynamicPrefixes.add(match[1]);
    }
  }
  return { used, dynamicPrefixes };
}

function main() {
  const errors = [];
  const enNs = listNamespaces('en');
  const viNs = listNamespaces('vi');
  if (enNs.join() !== viNs.join()) {
    errors.push(`namespace mismatch en=[${enNs}] vi=[${viNs}]`);
  }

  const en = loadLocale('en');
  const vi = loadLocale('vi');
  const enKeys = Object.keys(en).sort();
  const viKeys = Object.keys(vi).sort();

  for (const key of enKeys) {
    if (!Object.hasOwn(vi, key)) errors.push(`missing in vi: ${key}`);
    if (typeof en[key] !== 'string' || en[key].trim() === '') {
      errors.push(`empty en value: ${key}`);
    }
  }
  for (const key of viKeys) {
    if (!Object.hasOwn(en, key)) errors.push(`extra in vi: ${key}`);
    if (typeof vi[key] !== 'string' || vi[key].trim() === '') {
      errors.push(`empty vi value: ${key}`);
    }
  }

  if (!fixturesOnly) {
    const files = walkFiles(srcRoot);
    const { used, dynamicPrefixes } = collectUsedKeys(files, enNs);
    const catalogBases = new Set(enKeys.map(baseKey));

    for (const key of used) {
      if (!catalogBases.has(baseKey(key)) && !Object.hasOwn(en, key)) {
        errors.push(`referenced key missing from catalogs: ${key}`);
      }
    }

    for (const key of enKeys) {
      const base = baseKey(key);
      if (key.startsWith('errors.codes.') || key === 'errors.generic') continue;
      if ([...dynamicPrefixes].some((prefix) => base === prefix || base.startsWith(`${prefix}.`))) {
        continue;
      }
      if (!used.has(base) && !used.has(key)) {
        errors.push(`unused catalog key: ${key}`);
      }
    }

    if (errors.length === 0) {
      console.log(
        `check-i18n: ok (${String(enKeys.length)} keys, ${String(enNs.length)} namespaces, ${String(used.size)} referenced)`,
      );
      return;
    }
  } else if (errors.length === 0) {
    console.log(`check-i18n: ok (${String(enKeys.length)} keys)`);
    return;
  }

  console.error(`check-i18n: ${String(errors.length)} problem(s)`);
  for (const line of errors.slice(0, 50)) console.error(`  - ${line}`);
  if (errors.length > 50) console.error(`  … ${String(errors.length - 50)} more`);
  process.exitCode = 1;
}

main();
