import eslint from '@eslint/js';
import prettier from 'eslint-config-prettier';
import boundaries from 'eslint-plugin-boundaries';
import reactHooks from 'eslint-plugin-react-hooks';
import globals from 'globals';
import tseslint from 'typescript-eslint';

const layers = ['app', 'pages', 'features', 'entities', 'shared'];

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
  prettier,
);
