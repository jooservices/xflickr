import '@testing-library/jest-dom/vitest';
import { cleanup } from '@testing-library/react';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { afterAll, afterEach, beforeAll } from 'vitest';
import { server } from './server';

/** Paths of contracts/openapi, as the only routes a test may call (the mocked world must be real). */
const contract = readFileSync(
  resolve(__dirname, '../../../contracts/openapi/openapi.yaml'),
  'utf8',
);
const contractPaths = [...contract.matchAll(/^ {2}(\/[^:\n]+):/gm)].map(
  (match) => new RegExp(`^${match[1].replace(/\{[^}]+\}/g, '[^/]+')}$`),
);
const outsideContract: string[] = [];

beforeAll(() => {
  window.scrollTo = () => undefined;
  server.events.on('request:start', ({ request }) => {
    const { pathname } = new URL(request.url);
    if (['/api/health', '/mutator-test'].includes(pathname)) return; // tests of the mock server and of the fetch wrapper itself
    if (!contractPaths.some((pattern) => pattern.test(pathname))) outsideContract.push(pathname);
  });
  server.listen({ onUnhandledFrame: 'error' });
});

afterEach(() => {
  cleanup();
  window.localStorage.clear();
  document.documentElement.lang = '';
  server.resetHandlers();
  const bad = outsideContract.splice(0);
  if (bad.length > 0) {
    throw new Error(`The web called paths that are not in contracts/openapi: ${bad.join(', ')}`);
  }
});

afterAll(() => {
  server.close();
});
