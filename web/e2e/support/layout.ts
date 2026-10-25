import { expect } from '@playwright/test';
import type { Page } from '@playwright/test';

/**
 * Fails when the page scrolls sideways or two visible controls overlap. Controls are compared in pairs;
 * a control that contains another (a label wrapping its input) is not an overlap.
 */
export async function expectLayout(page: Page, screen: string): Promise<void> {
  const result = await page.evaluate(() => {
    const root = document.documentElement;
    const boxes: { name: string; x: number; y: number; right: number; bottom: number }[] = [];
    const controls = 'a[href], button, input, select, textarea, [role="button"], [role="link"]';
    for (const element of document.querySelectorAll<HTMLElement>(controls)) {
      const style = getComputedStyle(element);
      const rect = element.getBoundingClientRect();
      if (rect.width < 2 || rect.height < 2) continue;
      if (style.visibility === 'hidden' || style.display === 'none' || style.position === 'fixed')
        continue;
      const label =
        element.getAttribute('data-testid') ??
        element.textContent?.trim().slice(0, 24) ??
        element.tagName;
      boxes.push({ name: label, x: rect.x, y: rect.y, right: rect.right, bottom: rect.bottom });
    }
    const overlaps: string[] = [];
    for (let i = 0; i < boxes.length; i += 1) {
      for (let j = i + 1; j < boxes.length; j += 1) {
        const a = boxes[i];
        const b = boxes[j];
        const w = Math.min(a.right, b.right) - Math.max(a.x, b.x);
        const h = Math.min(a.bottom, b.bottom) - Math.max(a.y, b.y);
        if (w <= 2 || h <= 2) continue;
        const aInB =
          a.x >= b.x - 1 && a.y >= b.y - 1 && a.right <= b.right + 1 && a.bottom <= b.bottom + 1;
        const bInA =
          b.x >= a.x - 1 && b.y >= a.y - 1 && b.right <= a.right + 1 && b.bottom <= a.bottom + 1;
        if (!aInB && !bInA) overlaps.push(`${a.name} / ${b.name}`);
      }
    }
    return { scrollWidth: root.scrollWidth, clientWidth: root.clientWidth, overlaps };
  });
  expect(result.scrollWidth, `${screen} scrolls sideways`).toBeLessThanOrEqual(result.clientWidth);
  expect(result.overlaps, `${screen} has overlapping controls`).toEqual([]);
}
