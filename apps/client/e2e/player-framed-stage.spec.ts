// Stage contract: a framed Slide is scaled uniformly to its 16:9 stage, never clipped.
import { copyFileSync, mkdirSync } from "node:fs";
import { join } from "node:path";
import { test, expect, resetTokens } from "./accounts-fixture";

resetTokens();
test.afterEach(() => resetTokens());
const SHOTS =
  "/home/dat/.cache/tmp/claude-1000/-home-dat-dev-duolify/3c11ae02-db6a-44d9-9754-88e15e8d834c/scratchpad/immersive-shots";

for (const width of [390, 844, 1280]) {
  test(`framed Slide keeps 16:9 and fits its frame at ${width}px`, async ({
    page,
  }, info) => {
    await page.setViewportSize({ width, height: width === 390 ? 844 : 900 });
    await page.goto("/lesson/sky");
    const frame = page.locator('iframe[title="Slide 1"]');
    await expect(frame).toBeVisible();
    await page.waitForTimeout(400);
    const stage = (await page.locator(".stage-frame").boundingBox())!;
    const box = (await frame.boundingBox())!;
    expect(box.width / box.height).toBeCloseTo(16 / 9, 1);
    // the whole Slide is inside the stage: letterboxed, not clipped
    expect(box.x).toBeGreaterThanOrEqual(stage.x - 1);
    expect(box.y).toBeGreaterThanOrEqual(stage.y - 1);
    expect(box.x + box.width).toBeLessThanOrEqual(stage.x + stage.width + 1);
    expect(box.y + box.height).toBeLessThanOrEqual(stage.y + stage.height + 1);
    expect(stage.width).toBeLessThanOrEqual(width);
    const file = info.outputPath(`framed-${width}.png`);
    await page.screenshot({ path: file });
    try {
      mkdirSync(SHOTS, { recursive: true });
      copyFileSync(file, join(SHOTS, `framed-after-${width}.png`));
    } catch {
      /* owner copy is best effort */
    }
  });
}
