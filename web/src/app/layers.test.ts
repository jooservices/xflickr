import { describe, expect, it } from 'vitest';
import { entitiesLayer } from '../entities';
import { featuresLayer } from '../features';
import { sharedLayer } from '../shared';

describe('feature-sliced layers', () => {
  it('exposes the lower layers', () => {
    expect(featuresLayer).toBe('features');
    expect(entitiesLayer).toBe('entities');
    expect(sharedLayer).toBe('shared');
  });
});
