import { readFileSync } from 'node:fs';
import eslint from '@eslint/js';
import prettier from 'eslint-config-prettier';
import boundaries from 'eslint-plugin-boundaries';
import i18next from 'eslint-plugin-i18next';
import reactHooks from 'eslint-plugin-react-hooks';
import globals from 'globals';
import tseslint from 'typescript-eslint';
import glossary from './src/shared/i18n/glossary.json' with { type: 'json' };

const layers = ['app', 'pages', 'features', 'entities', 'shared'];

// Old files over the complexity limit, one "path rule" pair per line (see web/eslint-baseline.txt).
// The exemption is applied per file and rule; a new file or a new rule is never exempt.
// Component and function length and parameter count are review signals, not gates (JOOXF-516).
// XFLICKR_WEB_LINT_NO_BASELINE=1 ignores the baseline; docker/scripts/web-lint-report.sh uses it.
function readBaseline() {
  if (process.env.XFLICKR_WEB_LINT_NO_BASELINE === '1') {
    return [];
  }
  return readFileSync(new URL('./eslint-baseline.txt', import.meta.url), 'utf8')
    .split('\n')
    .map((line) => line.trim())
    .filter((line) => line !== '' && !line.startsWith('#'))
    .map((line) => {
      const [file, rule] = line.split(/\s+/);
      return { file, rule };
    });
}

const baseline = readBaseline();
const baselineBlocks = [...new Set(baseline.map((entry) => entry.rule))].map((rule) => ({
  files: baseline.filter((entry) => entry.rule === rule).map((entry) => entry.file),
  rules: { [rule]: 'off' },
}));

const layerElements = layers.map((layer) => ({
  type: layer,
  pattern: `src/${layer}`,
  partialMatch: false,
}));

function layersFrom(layer) {
  return layers.slice(layers.indexOf(layer));
}

export default tseslint.config(
  {
    ignores: [
      'dist/**',
      'coverage/**',
      'node_modules/**',
      'playwright-report/**',
      'test-results/**',
      'blob-report/**',
      'src/shared/__boundary_fixtures__/**',
      'src/shared/api/gen/**',
      'src/shared/api/zod/**',
    ],
  },
  {
    files: ['**/*.{ts,tsx}'],
    extends: [eslint.configs.recommended, ...tseslint.configs.strict],
    languageOptions: {
      ecmaVersion: 'latest',
      sourceType: 'module',
      globals: globals.browser,
    },
  },
  {
    ...reactHooks.configs.flat.recommended,
    files: ['**/*.{ts,tsx}'],
  },
  {
    files: ['src/**/*.{ts,tsx}'],
    rules: {
      complexity: ['error', 10],
      'react-hooks/rules-of-hooks': 'error',
    },
  },
  {
    files: ['**/*.{ts,tsx}'],
    plugins: { boundaries },
    settings: {
      'import/resolver': {
        node: {
          extensions: ['.js', '.jsx', '.ts', '.tsx'],
        },
      },
      'boundaries/elements': layerElements,
      'boundaries/dependency-nodes': ['import', 'export', 'dynamic-import'],
    },
    rules: {
      'boundaries/dependencies': [
        'error',
        {
          default: 'disallow',
          policies: layers.map((layer) => ({
            from: { element: { type: layer } },
            allow: {
              to: { element: { types: { anyOf: layersFrom(layer) } } },
            },
          })),
        },
      ],
    },
  },
  {
    files: ['**/*.test.{ts,tsx}', 'src/test/**/*.ts', 'vite.config.ts', 'playwright.config.ts'],
    languageOptions: {
      globals: {
        ...globals.browser,
        ...globals.node,
      },
    },
  },
  {
    files: ['src/**/*.{tsx,jsx}'],
    ignores: [
      'src/**/*.test.{ts,tsx}',
      'src/test/**',
      'src/shared/__boundary_fixtures__/**',
      'src/shared/api/gen/**',
    ],
    plugins: { i18next },
    rules: {
      'i18next/no-literal-string': [
        'error',
        {
          mode: 'jsx-text-only',
          words: {
            exclude: [
              '[0-9!-/:-@[-`{-~]+',
              '[A-Z_-]+',
              ...glossary.terms.map((term) => term.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')),
            ],
          },
        },
      ],
    },
  },
  ...baselineBlocks,
  prettier,
);
