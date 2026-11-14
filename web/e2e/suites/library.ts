import type { Page } from '@playwright/test';
import { expect } from '@playwright/test';
import { SEEDED_NSID } from '../support/env';
import { removeFavoriteLibrary, seedFavoriteLibrary } from '../support/factories';
import { expectNoErrorPage, monitor } from '../steps/monitor';
import {
  FAVORITES_PAGE,
  FAVORITES_TOTAL,
  clearSelection,
  clickLoadMore,
  clickSelectMode,
  clickSelectVisible,
  expectCaptionOnFocus,
  expectLibraryPhoto,
  expectLoadedFavorites,
  expectNoDownloadTray,
  expectPhotoSelected,
  expectPhotoSelectionDisabled,
  expectSelectModePressed,
  expectSelectionCount,
  expectSelectionLimitNotice,
  pressEscape,
  scrollToLibraryHeading,
  selectOwner,
  selectPhoto,
  watchLibraryRequests,
} from '../steps/library';
import { openPage } from '../steps/session';

async function loadEveryFavorite(page: Page): Promise<void> {
  for (let loaded = FAVORITES_PAGE; loaded < FAVORITES_TOTAL; loaded += FAVORITES_PAGE) {
    await clickLoadMore(page);
    await expectLoadedFavorites(page, Math.min(loaded + FAVORITES_PAGE, FAVORITES_TOTAL));
  }
}

/** On 501 seeded favorites of the connected account, selecting the visible favorites stops at the limit of 500 and keeps the filtered selection through leaving select mode. */
export async function selectFavoritesWithinTheLimit(page: Page): Promise<void> {
  seedFavoriteLibrary(SEEDED_NSID);
  const served = watchLibraryRequests(page);
  const watch = monitor(page);
  try {
    await openPage(page, '/library');
    await expectLibraryPhoto(page, 1);
    await expectCaptionOnFocus(page, 1);
    await expectLoadedFavorites(page, FAVORITES_PAGE);
    await loadEveryFavorite(page);
    expect(served.favoritesRequests()).toBe(6);

    await scrollToLibraryHeading(page);
    await clickSelectMode(page);
    await selectPhoto(page, 1);
    await clickSelectVisible(page);
    await expectSelectionCount(page, '500');
    await expectSelectionLimitNotice(page);

    await selectOwner(page, 'new@N00');
    await expectPhotoSelected(page, 500, true);
    await expectPhotoSelected(page, 501, false);
    await expectPhotoSelectionDisabled(page, 501);

    await pressEscape(page);
    await expectSelectModePressed(page, false);
    await clickSelectMode(page);
    await expectSelectionCount(page, '500');
    await expectPhotoSelected(page, 500, true);
    await expectPhotoSelected(page, 501, false);
    await clearSelection(page);
    await expectSelectionCount(page, '0');

    await expectNoDownloadTray(page);
    expect(served.favoritesRequests()).toBe(6);
    expect(served.downloadRequests()).toBe(0);
    await expectNoErrorPage(page, '/library');
    watch.expectClean('/library selection');
  } finally {
    watch.stop();
    removeFavoriteLibrary(SEEDED_NSID);
  }
}
