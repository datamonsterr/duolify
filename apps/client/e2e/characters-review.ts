import { mkdir } from "node:fs/promises";
import { join } from "node:path";
import type { Page } from "@playwright/test";

const reviewDir = process.env.CHARACTER_APPROVAL_DIR;

export const characterReviewViews = [
  { width: 1280, height: 800, color: "light" },
  { width: 390, height: 844, color: "dark" },
  ...(reviewDir
    ? ([
        { width: 1280, height: 800, color: "dark" },
        { width: 390, height: 844, color: "light" },
      ] as const)
    : []),
] as const;

export async function captureCharacterReview(
  page: Page,
  surface: string,
  { width, height, color }: (typeof characterReviewViews)[number],
) {
  if (!reviewDir) return;
  await mkdir(reviewDir, { recursive: true });
  await page.screenshot({
    path: join(reviewDir, `${surface}-${width}x${height}-${color}.png`),
    animations: "disabled",
  });
}
