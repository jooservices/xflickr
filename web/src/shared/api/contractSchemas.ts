import * as zod from 'zod';

/**
 * Response schemas for the calls the screens make that are not in contracts/openapi yet
 * (docs/ui/views/*.md, "Data and API"). They check the fields the UI reads, nothing more, and
 * live next to the generated schemas so `validate.ts` is the only place that applies them.
 */
const list = zod.array(zod.unknown());

export const usageSnapshot = zod.looseObject({ items: list });
export const shellSummary = zod.looseObject({ attention: list });
export const itemList = zod.looseObject({ items: list });
export const adminShell = zod.looseObject({ cluster: zod.looseObject({}) });
export const flickrAccounts = list;
export const wizardCrawlSettings = zod.looseObject({
  depth_max: zod.number(),
  depth_default: zod.number(),
});
export const crawlQueue = zod.looseObject({ lanes: list });
export const crawlSettings = zod.looseObject({ depth: zod.number(), max_depth: zod.number() });
export const crawlSummary = zod.looseObject({ contacts_total: zod.number() });
export const crawlJob = zod.looseObject({ id: zod.string() });
export const anyObject = zod.looseObject({});
