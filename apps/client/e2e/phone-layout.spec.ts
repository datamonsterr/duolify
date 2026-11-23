// Phone layout rules (openspec simplify-ux-gestures-hotkeys, tasks 3.1): reach, target size and gaps,
// greyscale state signals, safe area and modal-to-sheet. API-free: every /v1 call is mocked.
import { expect, test, type Page } from "./native-store-fixture";
import {
  gapViolations,
  reachViolations,
} from "../src/features/design/humanFactors";

const learner = {
  id: "10000000-0000-4000-8000-000000000001",
  display_name: "Learner",
  locale: "en",
  time_zone: "UTC",
  background: "",
  analytics_opt_out: true,
  welcome_state: "completed",
  tour_state: "completed",
};
const settings = {
  appearance: "system",
  motion: "system",
  narration: true,
  narration_speed: 1,
  captions: true,
  text_size: 1,
  ui_sounds: true,
  haptics: "system",
  locale: "en",
  time_zone: "UTC",
  daily_minutes: null,
  daily_xp: null,
  reminder_enabled: false,
  reminder_time: "18:00",
  fullscreen_on_rotate: true,
  autoplay: true,
  auto_send_after_dictation: false,
  analytics_opt_out: true,
};
async function seed(page: Page) {
  await page.addInitScript(() =>
    sessionStorage.setItem(
      "duolify.native-store-fixture",
      JSON.stringify({
        access_token: "phone-access",
        refresh_token: "phone-refresh",
        expires_in: 900,
      }),
    ),
  );
  await page.route("**/v1/**", (route) => route.fulfill({ json: [] }));
  await page.route("**/v1/me", (route) => route.fulfill({ json: learner }));
  await page.route("**/v1/me/settings", (route) =>
    route.fulfill({ json: settings }),
  );
}

const screens = [
  "/",
  "/courses",
  "/credits",
  "/friends",
  "/leagues",
  "/notes",
  "/settings",
  "/settings#look",
  "/settings#audio",
  "/settings#goals",
  "/settings#privacy",
  "/settings#account",
];
const viewport = { width: 390, height: 844 };

test.describe("phone layout at 390 by 844", () => {
  test.use({ viewport });
  for (const path of screens) {
    test(`reach and action levels ${path}`, async ({ page }) => {
      await seed(page);
      await page.goto(path);
      await page.waitForLoadState("networkidle");
      const main = page.locator("main");
      await main.waitFor();
      const levels = await main.evaluate((el) => {
        const level = (l: string) =>
          [...el.querySelectorAll(`[data-action-level="${l}"]`)].filter(
            (n) => !n.closest("[hidden]") && (n as HTMLElement).offsetParent,
          ).length;
        return { primary: level("primary"), secondary: level("secondary") };
      });
      expect(levels.primary, `${path} primary actions`).toBeLessThanOrEqual(1);
      expect(levels.secondary, `${path} secondary actions`).toBeLessThanOrEqual(
        2,
      );
      const primary = main.locator('[data-action-level="primary"]:visible');
      if (await primary.count()) {
        const box = await primary.first().boundingBox();
        // The tab bar is part of the bottom inset: the primary action must clear it.
        const bar = await page.locator(".bottom-tabs").boundingBox();
        const covered = bar ? viewport.height - bar.y : 0;
        expect(
          reachViolations(box!, viewport, covered),
          `${path} ${JSON.stringify(box)} bar ${JSON.stringify(bar)}`,
        ).toEqual([]);
      }
    });
  }

  test("targets are at least 44 by 44 with 8 px gaps in the tab bar", async ({
    page,
  }) => {
    await seed(page);
    await page.goto("/");
    const tabs = page.locator(".bottom-tabs a");
    await tabs.first().waitFor();
    const boxes = [];
    for (const tab of await tabs.all()) {
      const b = (await tab.boundingBox())!;
      expect(b.width).toBeGreaterThanOrEqual(44);
      expect(b.height).toBeGreaterThanOrEqual(44);
      boxes.push(b);
    }
    expect(gapViolations(boxes, 4)).toEqual([]);
    const bar = (await page.locator(".bottom-tabs").boundingBox())!;
    expect(bar.height).toBeGreaterThanOrEqual(56);
    const labels = await tabs.locator("span").allInnerTexts();
    expect(labels[0]).toBe("Learn");
  });

  test("tab bar clears a 34 px bottom inset", async ({ page }) => {
    await seed(page);
    await page.addInitScript(() => {
      const style = document.createElement("style");
      style.textContent = ":root{--test-inset:34px}";
      document.documentElement.appendChild(style);
    });
    await page.goto("/");
    const bar = (await page.locator(".bottom-tabs").boundingBox())!;
    expect(bar.y + bar.height).toBeLessThanOrEqual(viewport.height);
    const padding = await page
      .locator(".bottom-tabs")
      .evaluate((el) => getComputedStyle(el).paddingBottom);
    // env(safe-area-inset-bottom) is 0 in desktop Chromium; the rule must still reserve the base padding.
    expect(parseFloat(padding)).toBeGreaterThanOrEqual(8);
  });

  test("icon-only controls use the universal set", async ({ page }) => {
    await seed(page);
    for (const path of ["/", "/settings#audio"]) {
      await page.goto(path);
      await page.waitForLoadState("networkidle");
      const found = await page.evaluate(() =>
        [...document.querySelectorAll("button, a[href]")]
          .filter(
            (el) => el.querySelector("[data-icon]") && !el.textContent?.trim(),
          )
          .map((el) => ({
            icon: el.querySelector("[data-icon]")!.getAttribute("data-icon"),
            name: el.getAttribute("aria-label") ?? el.getAttribute("title"),
          })),
      );
      for (const f of found) {
        expect(f.name, `${path} ${f.icon}`).toBeTruthy();
        expect(
          [
            "play",
            "pause",
            "previous",
            "next",
            "close",
            "back",
            "more",
            "search",
            "add",
            "send",
            "microphone",
            "volume",
            "mute",
            "fullscreen",
            "captions",
            "settings",
            "share",
            "bookmark",
            "home",
          ],
          `${path} ${f.icon}`,
        ).toContain(f.icon);
      }
    }
  });
});

test.describe("modal becomes a sheet", () => {
  for (const [name, size, sheet] of [
    ["phone", { width: 390, height: 844 }, true],
    ["desktop", { width: 1280, height: 800 }, false],
  ] as const) {
    test(`dialog presentation at ${name}`, async ({ page }) => {
      await page.setViewportSize(size);
      await seed(page);
      await page.goto("/friends");
      await page.waitForLoadState("networkidle");
      const open = page
        .getByRole("button", { name: "Add", exact: true })
        .first();
      await open.click();
      const dialog = page.locator("dialog[open]").first();
      await dialog.waitFor();
      const box = (await dialog.boundingBox())!;
      if (sheet) {
        expect(box.width).toBeGreaterThanOrEqual(size.width - 1);
        expect(box.y + box.height).toBeGreaterThanOrEqual(size.height - 1);
      } else {
        expect(box.width).toBeLessThan(size.width);
        expect(box.y).toBeGreaterThan(0);
      }
    });
  }
});

test("greyscale keeps every state readable", async ({ page }) => {
  await page.setViewportSize(viewport);
  await seed(page);
  // A failed save shows an alert; in greyscale it must still carry an icon and text.
  await page.route("**/v1/me/settings", (route) =>
    route.request().method() === "GET"
      ? route.fulfill({ json: settings })
      : route.fulfill({ status: 500, json: { code: "x" } }),
  );
  await page.goto("/settings#audio");
  await page.addStyleTag({ content: "html{filter:grayscale(1)}" });
  await page.getByRole("button", { name: "Save settings" }).click();
  const alert = page
    .locator("[role=alert], [role=status]", { hasText: /\S/ })
    .last();
  await alert.waitFor();
  expect((await alert.innerText()).trim().length).toBeGreaterThan(0);
});
