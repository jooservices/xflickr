import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { ESLint } from 'eslint';
import { describe, expect, it } from 'vitest';

const webRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..');

async function lintFile(relativePath: string, ignore: boolean) {
  const eslint = new ESLint({
    cwd: webRoot,
    overrideConfigFile: path.join(webRoot, 'eslint.config.js'),
    ignore,
  });

  return eslint.lintFiles([path.join(webRoot, relativePath)]);
}

describe('layer boundaries', () => {
  it('rejects a shared module that imports the app layer', async () => {
    const results = await lintFile('src/shared/__boundary_fixtures__/imports-app.ts', false);
    const messages = results.flatMap((result) => result.messages);

    expect(results.some((result) => result.errorCount > 0)).toBe(true);
    expect(messages.some((message) => message.ruleId === 'boundaries/dependencies')).toBe(true);
  });

  it('accepts an app module that imports a lower layer', async () => {
    const results = await lintFile('src/app/App.tsx', true);
    const errors = results.flatMap((result) =>
      result.messages.filter((message) => message.severity === 2),
    );

    expect(errors).toEqual([]);
  });
});
