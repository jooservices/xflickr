import type { Page } from '@playwright/test';
import { expect } from '@playwright/test';
import { SEEDED_NSID } from '../support/env';
import { observeCrawledPhoto } from '../support/catalog';
import {
  albumsState,
  downloadItemIsActive,
  mediaFetchJobCount,
  personFirstSeen,
  personStateCount,
  requeueAlbums,
  seedMissingPerson,
} from '../support/factories';
import { apiWrite } from '../steps/api';
import {
  photoSizesStatus,
  personPhotoCount,
  readPersonPhotos,
  readPhotoOwner,
} from '../steps/catalog';
import { expectCrawlPage } from '../steps/crawl';
import { expectNoErrorPage, monitor } from '../steps/monitor';
import { openPage } from '../steps/session';

/** The catalog screens are read through their real authenticated boundary, with no SQL observation seed. */
export async function browseObservedPeopleAndPhotos(page: Page): Promise<void> {
  const watch = monitor(page);
  await openPage(page, '/crawl');
  await expectCrawlPage(page);
  await expect
    .poll(() => personPhotoCount(page, SEEDED_NSID), { timeout: 30_000 })
    .toBeGreaterThan(0);
  const listing = await readPersonPhotos(page, SEEDED_NSID);
  expect(listing.items.length).toBeGreaterThan(0);
  expect(listing.items.every((photo) => photo.owner_nsid === SEEDED_NSID)).toBe(true);
  const photo = listing.items[0];
  expect(photo).toBeDefined();
  expect(await readPhotoOwner(page, Number(photo?.id))).toBe(SEEDED_NSID);
  await openPage(page, '/crawl');
  await expectNoErrorPage(page, '/crawl after the crawl');
  watch.expectClean('/catalog observations');
}

/** A person the crawl finds missing completes its work and stays suppressed after the work is queued again. */
export async function suppressMissingPerson(page: Page): Promise<void> {
  const watch = monitor(page);
  const missing = '99999999@N99';
  seedMissingPerson(missing);
  await expect.poll(() => personStateCount(missing), { timeout: 30_000 }).toBe('1');
  const first = personFirstSeen(missing);
  requeueAlbums(missing);
  await expect.poll(() => albumsState(missing), { timeout: 30_000 }).toBe('done');
  expect(personFirstSeen(missing)).toBe(first);
  await openPage(page, '/crawl');
  await expectCrawlPage(page);
  watch.expectClean('/crawl with a deleted person');
}

/** Provisional photo sizes are confirmed only by an explicit download request, and a second request reuses them. */
export async function confirmProvisionalSizesByDownloadRequest(page: Page): Promise<void> {
  const watch = monitor(page);
  await openPage(page, '/crawl');
  await expectCrawlPage(page);
  const photoID = 52000000001;
  await expect.poll(() => observeCrawledPhoto(photoID), { timeout: 15_000 }).toBe(true);
  // Ordinary catalog discovery must leave the crawled listing provisional.
  await expect.poll(() => photoSizesStatus(page, photoID)).toBe('provisional');
  const created = await apiWrite(page, 'POST', '/downloads', {
    kind: 'tray',
    photo_ids: [photoID],
  });
  expect(created.status).toBe(201);
  expect(created.body).toMatchObject({ item_count: 1, skipped: 0 });
  await expect.poll(() => photoSizesStatus(page, photoID), { timeout: 15_000 }).toBe('confirmed');
  const downloadID = (created.body as { id: number }).id;
  expect(Number.isSafeInteger(downloadID)).toBe(true);
  await expect.poll(() => downloadItemIsActive(downloadID, photoID), { timeout: 15_000 }).toBe('t');
  expect(mediaFetchJobCount(downloadID)).toBe('1');
  // A second explicit request reuses confirmed sizes through the direct path.
  const repeated = await apiWrite(page, 'POST', '/downloads', {
    kind: 'tray',
    photo_ids: [photoID],
  });
  expect(repeated.status).toBe(201);
  const repeatedID = (repeated.body as { id: number }).id;
  expect(Number.isSafeInteger(repeatedID)).toBe(true);
  expect(downloadItemIsActive(repeatedID, photoID)).toBe('t');
  await expectNoErrorPage(page, '/crawl after a direct download request');
  watch.expectClean('on-demand sizes');
}
