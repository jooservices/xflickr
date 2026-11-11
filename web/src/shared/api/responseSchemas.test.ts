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

  it('describes bounded visible photo counts for contacts and people', () => {
    const description =
      'Count of photos visible to this user among at most 1,000 most recently seen photos';

    expect(contract.ListContactsResponse.shape.items.element.shape.photos_count.description).toBe(
      description,
    );
    expect(contract.ListPeopleResponse.shape.items.element.shape.photos_count.description).toBe(
      description,
    );
  });

  it.each(generated)('%s has a ROUTES entry', (name, schema) => {
    expect(
      ROUTES.some((r) => r.schema === schema),
      `${name} is generated from contracts/openapi but missing from ROUTES in validate.ts`,
    ).toBe(true);
  });
});
