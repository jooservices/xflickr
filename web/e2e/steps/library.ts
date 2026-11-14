import { expect } from '@playwright/test';
import type { Page } from '@playwright/test';

export const FAVORITES_TOTAL = 501;
export const FAVORITES_PAGE = 100;

/** Counts the requests the library makes to the real API: pages of favorites read, and download requests made. */
export function watchLibraryRequests(page: Page): {
  favoritesRequests(): number;
  downloadRequests(): number;
} {
  let favorites = 0;
  let downloads = 0;
  page.on('request', (request) => {
    const url = new URL(request.url());
    if (request.method() === 'GET' && url.pathname === '/favorites') favorites += 1;
    if (request.method() === 'POST' && url.pathname === '/downloads') downloads += 1;
  });
  return { favoritesRequests: () => favorites, downloadRequests: () => downloads };
}

export async function expectLibraryPhoto(page: Page, id: number): Promise<void> {
  await expect(page.getByTestId(`user-library-photo-${id}`)).toBeVisible();
}

export async function expectCaptionOnFocus(page: Page, id: number): Promise<void> {
  const photo = page.getByTestId(`user-library-photo-${id}`);
  await photo.focus();
  await expect(photo.getByTestId(`user-library-photo-caption-${id}`)).toBeVisible();
}

export async function expectLoadedFavorites(page: Page, loaded: number): Promise<void> {
  await expect(page.getByTestId('user-library-total')).toContainText(
    `${String(loaded)} favorite photos loaded`,
  );
}

export async function clickLoadMore(page: Page): Promise<void> {
  await page.getByTestId('user-library-load-more').click();
}

export async function scrollToLibraryHeading(page: Page): Promise<void> {
  await page.getByTestId('user-library-heading').scrollIntoViewIfNeeded();
}

export async function clickSelectMode(page: Page): Promise<void> {
  await page.getByTestId('user-library-select-mode').click();
}

export async function selectPhoto(page: Page, id: number): Promise<void> {
  await page.getByTestId(`user-library-photo-select-${id}`).check();
}

export async function clickSelectVisible(page: Page): Promise<void> {
  await page.getByTestId('user-library-select-visible').click();
}

export async function expectSelectionCount(page: Page, count: string): Promise<void> {
  await expect(page.getByTestId('user-library-selection-status')).toContainText(count);
}

export async function expectSelectionLimitNotice(page: Page): Promise<void> {
  await expect(
    page.getByText(
      'The selection limit is 500 photos. Selected photos have not been added to the download tray.',
    ),
  ).toBeVisible();
}

export async function selectOwner(page: Page, owner: string): Promise<void> {
  await page.getByRole('checkbox', { name: owner }).check();
}

export async function expectPhotoSelected(
  page: Page,
  id: number,
  selected: boolean,
): Promise<void> {
  const box = page.getByTestId(`user-library-photo-select-${id}`);
  if (selected) await expect(box).toBeChecked();
  else await expect(box).not.toBeChecked();
}

export async function expectPhotoSelectionDisabled(page: Page, id: number): Promise<void> {
  await expect(page.getByTestId(`user-library-photo-select-${id}`)).toBeDisabled();
}

export async function pressEscape(page: Page): Promise<void> {
  await page.keyboard.press('Escape');
}

export async function expectSelectModePressed(page: Page, pressed: boolean): Promise<void> {
  await expect(page.getByTestId('user-library-select-mode')).toHaveAttribute(
    'aria-pressed',
    String(pressed),
  );
}

export async function clearSelection(page: Page): Promise<void> {
  await page.getByTestId('user-library-clear-selection').click();
}

export async function expectNoDownloadTray(page: Page): Promise<void> {
  await expect(page.getByTestId('user-library-download-tray')).toHaveCount(0);
}
