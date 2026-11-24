// Lesson player on real phones: portrait stage, expand control, rotation entry.
// Mocked suite: real client build, API fixture lesson ("sky").
import { mkdirSync } from "node:fs";
import type { Page } from "@playwright/test";
import { test, expect, resetTokens } from "./accounts-fixture";

resetTokens();
test.afterEach(() => resetTokens());

const ANDROID_UA =
  "Mozilla/5.0 (Linux; Android 14; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Mobile Safari/537.36";
const SHOTS = process.env.PHONE_SHOTS;

const frame = (page: Page) => page.locator('iframe[title="Slide 1"]');
async function openLesson(page: Page) {
  await page.goto("/lesson/sky");
  await expect(frame(page)).toBeVisible();
}
async function snap(page: Page, name: string) {
  if (!SHOTS) return;
  mkdirSync(SHOTS, { recursive: true });
  await page.screenshot({ path: `${SHOTS}/${name}.png` });
}

for (const vp of [
  { w: 320, h: 640 },
  { w: 390, h: 844 },
]) {
  test.describe(`portrait ${vp.w}x${vp.h}`, () => {
    test.use({
      viewport: { width: vp.w, height: vp.h },
      hasTouch: true,
      isMobile: true,
      userAgent: ANDROID_UA,
    });

    test("the Slide stage is full-width 16:9 and visible above the fold", async ({
      page,
    }) => {
      await openLesson(page);
      const stage = (await page.locator(".stage-frame").boundingBox())!;
      expect(stage.width).toBeGreaterThanOrEqual(vp.w - 40);
      expect(stage.width / stage.height).toBeCloseTo(16 / 9, 1);
      // The whole stage is above the fold, high on the screen.
      expect(stage.y + stage.height).toBeLessThanOrEqual(vp.h * 0.6);
      expect(stage.y).toBeLessThan(vp.h * 0.3);
      await snap(page, `portrait-${vp.w}x${vp.h}`);
    });

    test("no stray Settings disclosure above the lesson header", async ({
      page,
    }) => {
      await openLesson(page);
      await expect(page.locator(".sidebar-tools")).toBeHidden();
    });

    test("the expand control is visible with a 44px target", async ({
      page,
    }) => {
      await openLesson(page);
      const btn = page.getByTestId("immersive-enter");
      await expect(btn).toBeVisible();
      const box = (await btn.boundingBox())!;
      expect(box.width).toBeGreaterThanOrEqual(44);
      expect(box.height).toBeGreaterThanOrEqual(44);
      const stage = (await page.locator(".stage-frame").boundingBox())!;
      expect(box.y + box.height).toBeLessThanOrEqual(
        stage.y + stage.height + 1,
      );
    });
  });
}

test.describe("rotation on a phone", () => {
  test.use({
    viewport: { width: 320, height: 640 },
    hasTouch: true,
    isMobile: true,
    userAgent: ANDROID_UA,
  });
  test("turning to landscape enters immersive mode by default", async ({
    page,
  }) => {
    await openLesson(page);
    await page.setViewportSize({ width: 640, height: 320 });
    await expect(page.locator(".player.immersive")).toBeVisible();
    await expect(page.getByText("Exit lesson")).toBeHidden();
    await snap(page, "landscape-640x320");
  });
  test("a WebView that reports a fine primary pointer on a hover-less touch device still enters on rotation", async ({
    page,
  }) => {
    // Android WebViews on emulators and some devices report (pointer: fine).
    await page.addInitScript(() => {
      const mm = window.matchMedia.bind(window);
      window.matchMedia = (q: string) => {
        const r = mm(q);
        if (/pointer:\s*coarse/.test(q))
          return Object.create(r, { matches: { value: false } });
        return r;
      };
    });
    await openLesson(page);
    await page.setViewportSize({ width: 640, height: 320 });
    await expect(page.locator(".player.immersive")).toBeVisible();
  });
});
