import { describe, expect, it } from 'vitest';
import * as contract from './zod/schemas';
import { ROUTES } from './validate';

const generated = Object.entries(contract).filter(
  ([name]) => /^[A-Z]\w*Response$/.test(name) && !name.endsWith('ItemResponse'),
);

describe('ROUTES against the generated OpenAPI schemas', () => {
  it('finds the generated responses', () => {
    expect(generated.length).toBeGreaterThan(20);
  });

  it.each(generated)('%s has a ROUTES entry', (name, schema) => {
    expect(
      ROUTES.some((r) => r.schema === schema),
      `${name} is generated from contracts/openapi but missing from ROUTES in validate.ts`,
    ).toBe(true);
  });
});
