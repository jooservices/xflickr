import { expect } from '@playwright/test';
import type { Page } from '@playwright/test';

export type PhotoListing = { items: Array<{ id: number; owner_nsid: string }> };

/** How many photos the person's listing holds, or -1 while the read is not 200 yet (a polling caller keeps waiting). */
export async function personPhotoCount(page: Page, nsid: string): Promise<number> {
  const response = await page.request.get(`/people/${encodeURIComponent(nsid)}/photos`);
  // Before the crawl makes the person visible the read answers 404.
  if (response.status() !== 200) return -1;
  return ((await response.json()) as { items: unknown[] }).items.length;
}

/** The JSON the person's photo screen is built on, read through the same authenticated request. */
export async function readPersonPhotos(page: Page, nsid: string): Promise<PhotoListing> {
  const response = await page.request.get(`/people/${encodeURIComponent(nsid)}/photos`);
  return (await response.json()) as PhotoListing;
}

export async function readPhotoOwner(page: Page, photoID: number): Promise<string> {
  const detail = await page.request.get(`/photos/${String(photoID)}`);
  expect(detail.status()).toBe(200);
  return ((await detail.json()) as { owner_nsid: string }).owner_nsid;
}

export async function photoSizesStatus(page: Page, photoID: number): Promise<string> {
  const response = await page.request.get(`/photos/${photoID}`);
  expect(response.status()).toBe(200);
  return ((await response.json()) as { sizes_status: string }).sizes_status;
}
