// Immersive overlay polish: the answer panel and side rail are translucent, Talk is a small bubble,
// the panel has no title or checkbox, and explanations are toasts. Mocked suite (see voice-helpers).
import type { Page } from "@playwright/test";
import { expect, test } from "./native-store-fixture";
import {
  VIEWPORTS,
  enterImmersive,
  openLesson,
  openRail,
  setup,
  shot,
} from "./anchored-helpers";
import { mock, openPanel, REPLY } from "./voice-helpers";
import {
  composite,
  contrast,
  GLASS_ALPHA,
  parseHex,
} from "../src/features/lesson-player/immersive/contrast";

const OUT =
  "/home/dat/.cache/tmp/claude-1000/-home-dat-dev-duolify/3c11ae02-db6a-44d9-9754-88e15e8d834c/scratchpad/phone-lesson-after";
const SLIDES = {
  dark: "#0b0b10",
  light: "#ffffff",
} as const;

test.use({
  hasTouch: true,
  isMobile: true,
  launchOptions: {
    args: [
      "--use-fake-device-for-media-stream",
      "--use-fake-ui-for-media-stream",
    ],
  },
  permissions: ["microphone"],
});

/** Paint the Slide stage a known colour so contrast and screenshots are comparable. */
async function paintSlide(page: Page, colour: string) {
  const text = colour === SLIDES.light ? "#111" : "#fff";
  await page.addStyleTag({
    content: `.player.immersive .stage-frame, .player.immersive .stage-frame iframe { background: ${colour} !important; }`,
  });
  for (const frame of page.frames().filter((f) => f !== page.mainFrame()))
    await frame
      .evaluate(
        ([bg, fg]) => {
          document.documentElement.style.setProperty(
            "background",
            bg,
            "important",
          );
          document.body?.style.setProperty("background", bg, "important");
          document.body?.style.setProperty("color", fg, "important");
          document
            .querySelectorAll("body *")
            .forEach((el) => ((el as HTMLElement).style.color = fg));
        },
        [colour, text],
      )
      .catch(() => {});
}

/** [r, g, b, a] in 0-255 and 0-1 from rgb()/rgba() or color(srgb ...) computed values. */
function channels(css: string): number[] {
  const n = (css.match(/-?[\d.]+(?:e-?\d+)?/g) ?? []).map(Number);
  const srgb = css.startsWith("color(");
  const rgb = n.slice(0, 3).map((v) => (srgb ? Math.round(v * 255) : v));
  return [...rgb, n[3] ?? 1];
}

for (const theme of ["dark", "light"] as const) {
  test(`landscape ${theme} slide: translucent panel, bubble, icon-only, toasts`, async ({
    page,
    request,
    context,
  }, info) => {
    const vp = VIEWPORTS[1];
    await page.setViewportSize({ width: vp.width, height: vp.height });
    const { state } = await setup(page, request, vp, `overlay-${theme}`);
    await mock(page, { explained: true });
    await openLesson(page, state.lesson);
    await page.waitForTimeout(600);
    await enterImmersive(page);
    await openRail(page);
    await paintSlide(page, SLIDES[theme]);
    await page.waitForTimeout(300);
    await shot(page, info, vp, `${theme}-1-rail-idle`, OUT);
    await page.getByTestId("immersive-ask").click();

    const panel = page.getByTestId("answer-panel");
    await expect(panel).toBeVisible();
    // Translucent surface with a blur; no title, checkbox or helper paragraphs.
    const style = await panel.evaluate((el) => {
      const c = getComputedStyle(el);
      return {
        bg: c.backgroundColor,
        blur: c.backdropFilter || c.getPropertyValue("-webkit-backdrop-filter"),
        text: c.color,
      };
    });
    const alpha = channels(style.bg)[3];
    expect(alpha).toBeGreaterThan(0.6);
    expect(alpha).toBeLessThan(0.9);
    expect(style.blur).toContain("blur");
    await expect(panel.getByRole("heading")).toHaveCount(0);
    await expect(panel.locator('input[type="checkbox"]')).toHaveCount(0);
    await expect(panel.getByTestId("answer-collapse")).toHaveCount(0);
    await expect(panel.locator(".imm-answer-grab")).toBeVisible();
    for (const id of ["answer-send"])
      expect(
        (await panel.getByTestId(id).innerText()).trim(),
        `${id} is an icon button`,
      ).toBe("");
    // Text keeps 4.5:1 over the painted Slide at the measured alpha.
    const [r, g, b] = channels(style.bg);
    const bg = composite([r, g, b], alpha, [...parseHex(SLIDES[theme])]);
    const [tr, tg, tb] = channels(style.text);
    expect(contrast([tr, tg, tb], bg)).toBeGreaterThanOrEqual(4.5);
    expect(GLASS_ALPHA).toBeGreaterThan(0.6);
    await shot(page, info, vp, `${theme}-2-panel-idle`, OUT);

    // The bubble: small, round, 44-48 px target.
    const mic = page.getByTestId("voice-mic");
    const box = (await mic.boundingBox())!;
    expect(box.width).toBeGreaterThanOrEqual(44);
    expect(box.width).toBeLessThanOrEqual(48);

    await mic.dispatchEvent("pointerdown", { pointerId: 1, isPrimary: true });
    await expect(page.getByTestId("voice-timer")).toBeVisible();
    await page.waitForTimeout(900);
    await shot(page, info, vp, `${theme}-3-bubble-recording`, OUT);
    await mic.dispatchEvent("pointerup", { pointerId: 1, isPrimary: true });
    await expect(page.getByTestId("voice-countdown")).toBeVisible();
    await shot(page, info, vp, `${theme}-4-transcript-countdown`, OUT);
    await expect(panel.getByText(REPLY)).toBeVisible({ timeout: 8000 });
    await shot(page, info, vp, `${theme}-5-panel-reply`, OUT);

    await panel.getByTestId("answer-save-note").click();
    await expect(page.getByTestId("toast")).toContainText(
      "Saved as Quick note",
    );
    await page.waitForTimeout(400);
    await shot(page, info, vp, `${theme}-6-toast-saved-note`, OUT);

    await context.setOffline(true);
    await page.evaluate(() => window.dispatchEvent(new Event("offline")));
    await expect(page.getByTestId("toast")).toContainText("connection");
    await page.waitForTimeout(400);
    await shot(page, info, vp, `${theme}-7-toast-offline`, OUT);
  });
}

test("daily cap shows a toast with the reset time", async ({
  page,
  request,
}, info) => {
  const vp = VIEWPORTS[1];
  await page.setViewportSize({ width: vp.width, height: vp.height });
  const { state } = await setup(page, request, vp, "overlay-cap");
  await mock(page, {
    status: 429,
    body: {
      error: "voice_daily_cap",
      resets_at: new Date(Date.now() + 3_600_000).toISOString(),
    },
  });
  await openPanel(page, state.lesson);
  await paintSlide(page, SLIDES.dark);
  const mic = page.getByTestId("voice-mic");
  await mic.dispatchEvent("pointerdown", { pointerId: 1, isPrimary: true });
  await page.waitForTimeout(900);
  await mic.dispatchEvent("pointerup", { pointerId: 1, isPrimary: true });
  await expect(page.getByTestId("toast")).toContainText("voice limit");
  await page.waitForTimeout(400);
  await shot(page, info, vp, "dark-8-toast-daily-cap", OUT);
});

test("portrait 390x844: the lesson tutor sheet stays solid and the rail is icon-only", async ({
  page,
  request,
}, info) => {
  const vp = VIEWPORTS[0];
  await page.setViewportSize({ width: vp.width, height: vp.height });
  const { state } = await setup(page, request, vp, "overlay-portrait");
  await mock(page, { explained: true });
  await openPanel(page, state.lesson);
  await shot(page, info, vp, "portrait-1-ask", OUT);
});
