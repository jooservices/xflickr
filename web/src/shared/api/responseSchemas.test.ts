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

  it('keeps contact photo counts out of the response until cached stats are available', () => {
    expect(contract.ListContactsResponse.shape.items.element.shape).not.toHaveProperty(
      'photos_count',
    );
    expect(contract.ListPeopleResponse.shape.items.element.shape.photos_count.description).toBe(
      'Count of photos visible to this user',
    );
  });

  it.each(generated)('%s has a ROUTES entry', (name, schema) => {
    expect(
      ROUTES.some((r) => r.schema === schema),
      `${name} is generated from contracts/openapi but missing from ROUTES in validate.ts`,
    ).toBe(true);
  });
});
