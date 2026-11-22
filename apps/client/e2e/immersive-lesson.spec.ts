// Immersive presentation of the lesson player (immersive-mobile-lesson-player 3.1–3.3).
// Mocked suite: real client build, API fixture lesson ("sky": 3 slides of 3 s).
import { copyFileSync, mkdirSync } from "node:fs";
import { join } from "node:path";
import type {
  APIRequestContext,
  CDPSession,
  Page,
  TestInfo,
} from "@playwright/test";
import { test, expect, resetTokens } from "./accounts-fixture";
import { apiBase } from "./servers";

resetTokens();
test.afterEach(() => resetTokens());

const OWNER_SHOTS =
  "/home/dat/.cache/tmp/claude-1000/-home-dat-dev-duolify/3c11ae02-db6a-44d9-9754-88e15e8d834c/scratchpad/immersive-shots";

const VIEWPORTS = [
  { name: "portrait-390x844", width: 390, height: 844, touch: true },
  { name: "landscape-844x390", width: 844, height: 390, touch: true },
  { name: "desktop-1280x800", width: 1280, height: 800, touch: false },
] as const;
type Vp = (typeof VIEWPORTS)[number];

async function shot(page: Page, info: TestInfo, vp: Vp, name: string) {
  const file = info.outputPath(`${vp.name}-${name}.png`);
  await page.screenshot({ path: file });
  try {
    mkdirSync(OWNER_SHOTS, { recursive: true });
    copyFileSync(file, join(OWNER_SHOTS, `${vp.name}-${name}.png`));
  } catch {
    /* owner copy is best effort (CI has no scratchpad) */
  }
}

const frame = (page: Page, n = 1) => page.locator(`iframe[title="Slide ${n}"]`);
const controls = (page: Page) => page.getByTestId("immersive-controls");
const enterButton = (page: Page) => page.getByTestId("immersive-enter");

/** Per-Learner settings live on the API: store them there before opening the lesson. */
async function setSettings(
  page: Page,
  request: APIRequestContext,
  fields: Record<string, boolean>,
) {
  await page.goto("/profile");
  const token = await page.evaluate(
    () =>
      JSON.parse(sessionStorage.getItem("duolify.native-store-fixture")!)
        .access_token,
  );
  const r = await request.post(`${apiBase}/v1/me/settings`, {
    headers: { Authorization: `Bearer ${token}` },
    data: { confirmed: true, fields },
  });
  expect(r.ok()).toBe(true);
}

async function openLesson(page: Page) {
  await page.goto("/lesson/sky");
  await expect(frame(page)).toBeVisible();
  await expect(frame(page)).toHaveAttribute("sandbox", "allow-scripts");
  await expect(
    page.getByRole("status").filter({ hasText: "Playing" }),
  ).toBeVisible();
}

/** Slide clock in ms, read from the framed player's (possibly hidden) slider. */
const clock = (page: Page) =>
  page.evaluate(() =>
    Number(
      (
        document.querySelector(
          'input[aria-label="Slide time"]',
        ) as HTMLInputElement
      ).value,
    ),
  );

async function trackFrame(page: Page) {
  await page.evaluate(() => {
    const w = window as unknown as Record<string, unknown>;
    const f = document.querySelector("iframe")!;
    w.__frame = f;
    w.__loads = 0;
    w.__srcdoc = f.srcdoc;
    f.addEventListener("load", () => (w.__loads as number)++);
  });
}
const frameState = (page: Page) =>
  page.evaluate(() => {
    const w = window as unknown as Record<string, unknown>;
    const f = document.querySelector("iframe")!;
    return {
      same: f === w.__frame,
      loads: w.__loads as number,
      sandbox: f.getAttribute("sandbox"),
      srcdocSame: f.srcdoc === w.__srcdoc,
    };
  });

/** Fixture Slides last 3 s, as long as the auto-hide delay: pause so a Slide's end cannot pin the controls. */
async function pauseFramed(page: Page) {
  await page.getByRole("button", { name: "Pause", exact: true }).click();
  await expect(
    page.getByRole("status").filter({ hasText: "Paused" }),
  ).toBeVisible();
}

async function enter(page: Page) {
  await enterButton(page).click();
  await expect(page.locator(".player.immersive")).toBeVisible();
  await expect(page.getByTestId("immersive-layer")).toBeVisible();
}

async function geometry(page: Page) {
  return page.evaluate(() => {
    const root = document.documentElement;
    return {
      scale: Number(root.style.getPropertyValue("--imm-scale") || 1),
      rotated: root.dataset.immRotated === "true",
      w: window.innerWidth,
      h: window.innerHeight,
    };
  });
}

/** Screen point for a normalized stage point, honouring scale, letterbox and rotation. */
async function stagePoint(page: Page, nx: number, ny: number) {
  const g = await geometry(page);
  const sx = (nx - 0.5) * 1280 * g.scale;
  const sy = (ny - 0.5) * 720 * g.scale;
  return g.rotated
    ? { x: g.w / 2 - sy, y: g.h / 2 + sx }
    : { x: g.w / 2 + sx, y: g.h / 2 + sy };
}

async function tapAt(page: Page, vp: Vp, x: number, y: number) {
  if (vp.touch) await page.touchscreen.tap(x, y);
  else await page.mouse.click(x, y);
}
// Off-centre by default: the centre of the stage holds the play controls.
async function tapStage(page: Page, vp: Vp, nx = 0.25, ny = 0.3) {
  const p = await stagePoint(page, nx, ny);
  await tapAt(page, vp, p.x, p.y);
}
async function doubleTapStage(page: Page, vp: Vp, nx: number, ny = 0.5) {
  const p = await stagePoint(page, nx, ny);
  await tapAt(page, vp, p.x, p.y);
  await page.waitForTimeout(60);
  await tapAt(page, vp, p.x, p.y);
}

async function dragStage(
  page: Page,
  vp: Vp,
  from: { x: number; y: number },
  to: { x: number; y: number },
  cdp?: CDPSession,
) {
  const a = await stagePoint(page, from.x, from.y);
  const b = await stagePoint(page, to.x, to.y);
  const steps = 6;
  if (vp.touch && cdp) {
    await cdp.send("Input.dispatchTouchEvent", {
      type: "touchStart",
      touchPoints: [a],
    });
    for (let i = 1; i <= steps; i++)
      await cdp.send("Input.dispatchTouchEvent", {
        type: "touchMove",
        touchPoints: [
          {
            x: a.x + ((b.x - a.x) * i) / steps,
            y: a.y + ((b.y - a.y) * i) / steps,
          },
        ],
      });
    await cdp.send("Input.dispatchTouchEvent", {
      type: "touchEnd",
      touchPoints: [],
    });
  } else {
    await page.mouse.move(a.x, a.y);
    await page.mouse.down();
    await page.mouse.move(b.x, b.y, { steps });
    await page.mouse.up();
  }
}

const settle = (page: Page) => page.waitForTimeout(450); // past the double-tap interval

for (const vp of VIEWPORTS) {
  test.describe(`immersive lesson at ${vp.name}`, () => {
    test.use({
      viewport: { width: vp.width, height: vp.height },
      hasTouch: vp.touch,
      isMobile: vp.touch,
    });

    test("enter and leave without losing time, the Slide or the clock", async ({
      page,
    }, info) => {
      await openLesson(page);
      await shot(page, info, vp, "framed-with-expand-control");
      await page.waitForTimeout(500);
      await trackFrame(page);
      const before = await clock(page);
      expect(before).toBeGreaterThan(0);
      await enter(page);
      await page.waitForTimeout(250);
      const during = await clock(page);
      expect(during).toBeGreaterThanOrEqual(before);
      expect(during - before).toBeLessThan(2500);
      expect(await frameState(page)).toEqual({
        same: true,
        loads: 0,
        sandbox: "allow-scripts",
        srcdocSame: true,
      });
      await shot(page, info, vp, "immersive-controls-visible");
      await page.getByTestId("immersive-exit").click();
      await expect(page.locator(".player.immersive")).toHaveCount(0);
      expect(await frameState(page)).toEqual({
        same: true,
        loads: 0,
        sandbox: "allow-scripts",
        srcdocSame: true,
      });
      expect(await clock(page)).toBeGreaterThanOrEqual(during);
      await expect(enterButton(page)).toBeFocused();
      await expect(
        page.getByRole("status").filter({ hasText: "Immersive mode off" }),
      ).toBeVisible();
    });

    test("stage is 16:9, uniformly scaled and letterboxed inside the screen", async ({
      page,
    }, info) => {
      await openLesson(page);
      await enter(page);
      const box = (await frame(page).boundingBox())!;
      const g = await geometry(page);
      expect(g.rotated).toBe(vp.touch && vp.height > vp.width);
      const long = Math.max(box.width, box.height);
      const short = Math.min(box.width, box.height);
      expect(long / short).toBeCloseTo(16 / 9, 1);
      // rotated: the stage's long edge follows the screen's long edge
      if (g.rotated) expect(box.height).toBeGreaterThan(box.width);
      else expect(box.width).toBeGreaterThan(box.height);
      expect(box.x).toBeGreaterThanOrEqual(-1);
      expect(box.y).toBeGreaterThanOrEqual(-1);
      expect(box.x + box.width).toBeLessThanOrEqual(vp.width + 1);
      expect(box.y + box.height).toBeLessThanOrEqual(vp.height + 1);
      await shot(
        page,
        info,
        vp,
        g.rotated ? "stage-rotated-fallback" : "stage-letterboxed",
      );
    });

    test("tap shows and hides the controls; they hide on their own after 3 s", async ({
      page,
    }, info) => {
      await openLesson(page);
      await pauseFramed(page);
      await enter(page);
      await expect(controls(page)).toHaveAttribute("data-visible", "true");
      await page.waitForTimeout(3300);
      await expect(controls(page)).toHaveAttribute("data-visible", "false");
      await shot(page, info, vp, "controls-hidden");
      await expect(
        page.getByTestId("immersive-side-rail-handle"),
      ).toBeVisible();
      await tapStage(page, vp);
      await expect(controls(page)).toHaveAttribute("data-visible", "true");
      await settle(page);
      await tapStage(page, vp);
      await expect(controls(page)).toHaveAttribute("data-visible", "false");
    });

    test("replay this Slide restarts the Slide and its clock, then controls hide", async ({
      page,
    }) => {
      await openLesson(page);
      await enter(page);
      await page.waitForTimeout(1500);
      const t = await clock(page);
      expect(t).toBeGreaterThan(1000);
      await page.getByTestId("immersive-replay").click();
      await page.waitForTimeout(100);
      expect(await clock(page)).toBeLessThan(t - 800);
      await expect(page.getByTestId("immersive-state")).toHaveText("Playing");
      await page.getByTestId("immersive-play").click(); // pause: the Slide's end would pin the layer
      await page.waitForTimeout(3300);
      await expect(controls(page)).toHaveAttribute("data-visible", "false");
    });

    test("previous and next Slide with the control layer", async ({
      page,
    }, info) => {
      await openLesson(page);
      await enter(page);
      await expect(page.getByTestId("immersive-previous")).toBeDisabled();
      await page.getByTestId("immersive-next").click();
      await expect(frame(page, 2)).toBeAttached();
      await expect(page.locator(".player.immersive")).toBeVisible();
      await expect(page.getByTestId("immersive-announcer")).toHaveText(
        "Slide 2 of 3",
      );
      await page.getByTestId("immersive-next").click();
      await expect(frame(page, 3)).toBeAttached();
      await expect(page.getByTestId("immersive-take-test")).toBeVisible();
      await expect(page.getByTestId("immersive-next")).toHaveCount(0);
      await shot(page, info, vp, "last-slide-take-test");
      await page.getByTestId("immersive-previous").click();
      await expect(frame(page, 2)).toBeAttached();
      await expect(page.getByTestId("immersive-time")).toContainText("/");
      await expect(page.getByTestId("immersive-segment")).toHaveCount(3);
      await shot(page, info, vp, "progress-segmented");
    });

    test("autoplay off stops at the end with controls kept; on counts down and can be cancelled", async ({
      page,
      request,
    }, info) => {
      await setSettings(page, request, { autoplay: false });
      await openLesson(page);
      await enter(page);
      const auto = page.getByTestId("immersive-autoplay");
      await expect(auto).toHaveAttribute("aria-pressed", "false");
      // off: the Slide ends, nothing advances, controls stay up past 3 s
      await expect(page.getByTestId("immersive-state")).toContainText(
        "Slide ended",
        {
          timeout: 7000,
        },
      );
      await page.waitForTimeout(3500);
      await expect(controls(page)).toHaveAttribute("data-visible", "true");
      await expect(page.getByTestId("immersive-countdown")).toHaveCount(0);
      await expect(frame(page, 1)).toBeAttached();
      await shot(page, info, vp, "end-of-slide-autoplay-off");
      // on + cancel
      await auto.click();
      await expect(auto).toHaveAttribute("aria-pressed", "true");
      await page.getByTestId("immersive-replay").click();
      await expect(page.getByTestId("immersive-countdown")).toBeVisible({
        timeout: 7000,
      });
      await shot(page, info, vp, "autoplay-countdown");
      await page.getByTestId("immersive-countdown-cancel").click();
      await expect(page.getByTestId("immersive-countdown")).toHaveCount(0);
      await page.waitForTimeout(3600);
      await expect(frame(page, 1)).toBeAttached();
      // on + let it finish
      await page.getByTestId("immersive-replay").click();
      await expect(page.getByTestId("immersive-countdown")).toBeVisible({
        timeout: 7000,
      });
      await expect(frame(page, 2)).toBeAttached({ timeout: 6000 });
      await expect(page.locator(".player.immersive")).toBeVisible();
    });

    test("double tap right and left steps Slides without toggling the controls", async ({
      page,
    }) => {
      await openLesson(page);
      await pauseFramed(page);
      await enter(page);
      await page.waitForTimeout(3300);
      await expect(controls(page)).toHaveAttribute("data-visible", "false");
      await doubleTapStage(page, vp, 0.8);
      await expect(frame(page, 2)).toBeAttached();
      await settle(page);
      await expect(controls(page)).toHaveAttribute("data-visible", "false");
      await doubleTapStage(page, vp, 0.2);
      await expect(frame(page, 1)).toBeAttached();
      await settle(page);
      await expect(controls(page)).toHaveAttribute("data-visible", "false");
    });

    test("horizontal swipe steps Slides", async ({ page }) => {
      await openLesson(page);
      await enter(page);
      const cdp = vp.touch
        ? await page.context().newCDPSession(page)
        : undefined;
      await dragStage(page, vp, { x: 0.75, y: 0.5 }, { x: 0.25, y: 0.5 }, cdp);
      await expect(frame(page, 2)).toBeAttached();
      await dragStage(page, vp, { x: 0.25, y: 0.5 }, { x: 0.75, y: 0.5 }, cdp);
      await expect(frame(page, 1)).toBeAttached();
      // a mostly vertical drag is not a swipe
      await dragStage(page, vp, { x: 0.5, y: 0.2 }, { x: 0.55, y: 0.9 }, cdp);
      await settle(page);
      await expect(frame(page, 1)).toBeAttached();
    });

    test("lock disables gestures and controls until the unlock target is held", async ({
      page,
    }, info) => {
      await openLesson(page);
      await enter(page);
      await page.getByTestId("immersive-lock").click();
      await expect(page.getByTestId("immersive-unlock")).toBeVisible();
      await expect(controls(page)).toHaveCount(0);
      await expect(page.getByTestId("immersive-side-rail")).toHaveCount(0);
      await shot(page, info, vp, "locked");
      await tapStage(page, vp);
      await settle(page);
      await doubleTapStage(page, vp, 0.8);
      await settle(page);
      const cdp = vp.touch
        ? await page.context().newCDPSession(page)
        : undefined;
      await dragStage(page, vp, { x: 0.75, y: 0.5 }, { x: 0.25, y: 0.5 }, cdp);
      await settle(page);
      await expect(frame(page, 1)).toBeAttached();
      await expect(page.getByTestId("immersive-unlock")).toBeVisible();
      // a short press does not unlock
      const box = (await page.getByTestId("immersive-unlock").boundingBox())!;
      await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
      await page.mouse.down();
      await page.waitForTimeout(200);
      await page.mouse.up();
      await expect(page.getByTestId("immersive-unlock")).toBeVisible();
      // holding does
      await page.mouse.down();
      await page.waitForTimeout(900);
      await page.mouse.up();
      await expect(page.getByTestId("immersive-unlock")).toHaveCount(0);
      await expect(controls(page)).toHaveAttribute("data-visible", "true");
    });

    test("system back and Escape leave immersive mode on the same Slide and time", async ({
      page,
    }) => {
      await openLesson(page);
      await page.waitForTimeout(400);
      const url = page.url();
      await trackFrame(page);
      await enter(page);
      await page.goBack();
      await expect(page.locator(".player.immersive")).toHaveCount(0);
      await expect
        .poll(() => page.evaluate(() => !document.fullscreenElement))
        .toBe(true);
      expect(page.url()).toBe(url);
      expect(await frameState(page)).toMatchObject({ same: true, loads: 0 });
      await enter(page);
      await page.keyboard.press("Escape");
      await expect(page.locator(".player.immersive")).toHaveCount(0);
      await expect
        .poll(() => page.evaluate(() => !document.fullscreenElement))
        .toBe(true);
      expect(page.url()).toBe(url);
      expect(await frameState(page)).toMatchObject({ same: true, loads: 0 });
      // history is not polluted: one back leaves the lesson, not an immersive entry
      await enter(page);
      await page.getByTestId("immersive-exit").click();
      await expect(page.locator(".player.immersive")).toHaveCount(0);
      await expect
        .poll(() => page.evaluate(() => !document.fullscreenElement))
        .toBe(true);
      expect(page.url()).toBe(url);
    });

    test("slide list navigates and Escape closes it before leaving", async ({
      page,
    }, info) => {
      await openLesson(page);
      await enter(page);
      await page.getByTestId("immersive-slides").click();
      const list = page.getByTestId("immersive-slide-list");
      await expect(list).toBeVisible();
      await expect(list.getByRole("button")).toHaveCount(3);
      await shot(page, info, vp, "slide-list");
      await page.keyboard.press("Escape");
      await expect(list).toHaveCount(0);
      await expect(page.locator(".player.immersive")).toBeVisible();
      await page.getByTestId("immersive-slides").click();
      await list.getByRole("button").nth(2).click();
      await expect(frame(page, 3)).toBeAttached();
    });

    test("side rail handle is reachable while controls are hidden and its slot holds the note tools", async ({
      page,
    }, info) => {
      await openLesson(page);
      await pauseFramed(page);
      await enter(page);
      await page.waitForTimeout(3300);
      const handle = page.getByTestId("immersive-side-rail-handle");
      const box = (await handle.boundingBox())!;
      expect(box.width).toBeGreaterThanOrEqual(44);
      expect(box.height).toBeGreaterThanOrEqual(44);
      await handle.click();
      await expect(handle).toHaveAttribute("aria-expanded", "true");
      await expect(page.getByTestId("immersive-side-rail-slot")).toBeVisible();
      await expect(page.getByTestId("immersive-select-area")).toBeVisible();
      await expect(page.getByTestId("immersive-quick-note")).toBeVisible();
      await shot(page, info, vp, "side-rail-open");
    });

    test("bookmark, captions, speed and narration reuse the player's actions", async ({
      page,
    }) => {
      await openLesson(page);
      await enter(page);
      const bookmark = page.getByTestId("immersive-bookmark");
      await bookmark.click();
      await expect(bookmark).toHaveAttribute("aria-pressed", "true");
      await bookmark.click();
      await expect(bookmark).toHaveAttribute("aria-pressed", "false");
      const speed = page.getByTestId("immersive-speed");
      await expect(speed).toHaveAccessibleName("Speed 1×");
      await speed.click();
      await expect(speed).toHaveAccessibleName("Speed 1.25×");
      await expect(page.getByTestId("immersive-caption")).toBeVisible();
      await page.getByTestId("immersive-captions").click();
      await expect(page.getByTestId("immersive-caption")).toHaveCount(0);
      const narration = page.getByTestId("immersive-narration");
      await expect(narration).toHaveAttribute("aria-pressed", "true");
      await narration.click();
      await expect(narration).toHaveAttribute("aria-pressed", "false");
      // the framed player sees the same state
      await page.getByTestId("immersive-exit").click();
      await expect(
        page.getByRole("button", { name: "Speed 1.25x" }),
      ).toBeVisible();
    });

    test("every control has an accessible name and a 44×44 target", async ({
      page,
    }) => {
      await openLesson(page);
      await enter(page);
      const ids = [
        "exit",
        "bookmark",
        "previous",
        "play",
        "next",
        "replay",
        "autoplay",
        "speed",
        "captions",
        "narration",
        "slides",
        "rotate",
        "lock",
      ];
      for (const id of ids) {
        const b = page.getByTestId(`immersive-${id}`);
        await expect(b).toHaveAccessibleName(/\S/);
        const box = (await b.boundingBox())!;
        expect(box.width, id).toBeGreaterThanOrEqual(43.5);
        expect(box.height, id).toBeGreaterThanOrEqual(43.5);
      }
    });

    test("play/pause toggles the shared clock and wake lock follows playback", async ({
      page,
    }) => {
      await page.addInitScript(() => {
        const log: string[] = [];
        (window as unknown as { __wl: string[] }).__wl = log;
        Object.defineProperty(navigator, "wakeLock", {
          configurable: true,
          value: {
            request: async () => {
              log.push("request");
              return { release: async () => void log.push("release") };
            },
          },
        });
      });
      const wl = () =>
        page.evaluate(() =>
          (window as unknown as { __wl: string[] }).__wl.join(","),
        );
      await openLesson(page);
      expect(await wl()).toBe("");
      await enter(page);
      await expect.poll(wl).toBe("request");
      await page.getByTestId("immersive-play").click(); // pause
      await expect.poll(wl).toBe("request,release");
      const t = await clock(page);
      await page.waitForTimeout(500);
      expect(await clock(page)).toBe(t);
      await page.getByTestId("immersive-play").click(); // play
      await expect.poll(wl).toBe("request,release,request");
      await page.getByTestId("immersive-exit").click();
      await expect.poll(wl).toBe("request,release,request,release");
    });

    test("safe-area insets move the controls, never the Slide", async ({
      page,
    }) => {
      await openLesson(page);
      await enter(page);
      const slide = (await frame(page).boundingBox())!;
      await page.evaluate(() => {
        const r = document.documentElement.style;
        r.setProperty("--imm-inset-top", "32px");
        r.setProperty("--imm-inset-left", "40px");
        r.setProperty("--imm-inset-right", "40px");
        r.setProperty("--imm-inset-bottom", "20px");
      });
      const rotated = (await geometry(page)).rotated;
      const exit = (await page.getByTestId("immersive-exit").boundingBox())!;
      if (rotated)
        expect(exit.x + exit.width).toBeLessThanOrEqual(vp.width - 20 + 1);
      else expect(exit.y).toBeGreaterThanOrEqual(32);
      expect(await frame(page).boundingBox()).toEqual(slide);
    });

    test("two-finger spread with no consumer does nothing visible", async ({
      page,
    }) => {
      test.skip(!vp.touch, "multi-touch needs a touch device");
      await openLesson(page);
      await pauseFramed(page);
      await enter(page);
      await page.waitForTimeout(3300);
      const cdp = await page.context().newCDPSession(page);
      const a = await stagePoint(page, 0.45, 0.45);
      const b = await stagePoint(page, 0.55, 0.55);
      const a2 = await stagePoint(page, 0.25, 0.25);
      const b2 = await stagePoint(page, 0.75, 0.75);
      await cdp.send("Input.dispatchTouchEvent", {
        type: "touchStart",
        touchPoints: [
          { ...a, id: 1 },
          { ...b, id: 2 },
        ],
      });
      for (let i = 1; i <= 5; i++)
        await cdp.send("Input.dispatchTouchEvent", {
          type: "touchMove",
          touchPoints: [
            {
              x: a.x + ((a2.x - a.x) * i) / 5,
              y: a.y + ((a2.y - a.y) * i) / 5,
              id: 1,
            },
            {
              x: b.x + ((b2.x - b.x) * i) / 5,
              y: b.y + ((b2.y - b.y) * i) / 5,
              id: 2,
            },
          ],
        });
      await cdp.send("Input.dispatchTouchEvent", {
        type: "touchEnd",
        touchPoints: [],
      });
      await settle(page);
      await expect(controls(page)).toHaveAttribute("data-visible", "false");
      await expect(frame(page, 1)).toBeAttached();
      await expect(page.locator(".player.immersive")).toBeVisible();
    });

    test("keyboard-only: enter, tab through controls in order, never auto-hide while focused, Escape returns focus", async ({
      page,
    }, info) => {
      test.skip(vp.touch, "keyboard run uses the desktop viewport");
      await openLesson(page);
      await enterButton(page).focus();
      await page.keyboard.press("Enter");
      await expect(page.locator(".player.immersive")).toBeVisible();
      await expect(page.getByTestId("immersive-play")).toBeFocused();
      // logical order, starting from the top of the layer
      await page.getByTestId("immersive-exit").focus();
      const order: string[] = [];
      for (let i = 0; i < 12; i++) {
        order.push(
          await page.evaluate(
            () => document.activeElement?.getAttribute("data-testid") ?? "",
          ),
        );
        await page.keyboard.press("Tab");
      }
      expect(order).toEqual([
        "immersive-exit",
        "immersive-bookmark",
        "immersive-play",
        "immersive-next",
        "immersive-replay",
        "immersive-autoplay",
        "immersive-speed",
        "immersive-captions",
        "immersive-narration",
        "immersive-slides",
        "immersive-rotate",
        "immersive-lock",
      ]);
      await expect(
        page.getByTestId("immersive-side-rail-handle"),
      ).toBeFocused();
      // focus inside the controls: they stay up well past 3 s
      await page.getByTestId("immersive-speed").focus();
      await page.waitForTimeout(4000);
      await expect(controls(page)).toHaveAttribute("data-visible", "true");
      await shot(page, info, vp, "keyboard-focus");
      // focus survives a Slide change
      await page.getByTestId("immersive-next").focus();
      await page.keyboard.press("Enter");
      await expect(frame(page, 2)).toBeAttached();
      await expect(page.getByTestId("immersive-next")).toBeFocused();
      await page.keyboard.press("Escape");
      await expect(page.locator(".player.immersive")).toHaveCount(0);
      await expect(enterButton(page)).toBeFocused();
    });

    test("hidden controls are not focusable; Tab brings them back", async ({
      page,
    }) => {
      test.skip(vp.touch, "keyboard run uses the desktop viewport");
      await openLesson(page);
      await pauseFramed(page);
      await enter(page);
      await page.waitForTimeout(3300);
      await expect(controls(page)).toHaveAttribute("data-visible", "false");
      await page.locator("body").press("Tab");
      await expect(controls(page)).toHaveAttribute("data-visible", "true");
      await expect(page.getByTestId("immersive-play")).toBeFocused();
    });

    test("reduced motion removes the countdown animation and control transitions", async ({
      page,
    }) => {
      await page.emulateMedia({ reducedMotion: "reduce" });
      await openLesson(page);
      await enter(page);
      await expect(page.getByTestId("immersive-countdown")).toBeVisible({
        timeout: 7000,
      });
      await expect(page.getByTestId("immersive-countdown-count")).toContainText(
        "Next slide in",
      );
      await expect(page.getByTestId("immersive-countdown-ring")).toHaveCount(0);
      const duration = await controls(page).evaluate(
        (el) => getComputedStyle(el).transitionDuration,
      );
      expect(duration).toMatch(/^0s$|^0s, 0s/);
    });

    test("an offline downloaded lesson plays in immersive mode", async ({
      page,
      context,
    }, info) => {
      await openLesson(page);
      await expect(page.getByText("Available offline")).toBeVisible();
      await page.evaluate(async () => {
        await navigator.serviceWorker.ready;
      });
      await context.setOffline(true);
      await page.route("**/v1/**", (route) =>
        route.abort("internetdisconnected"),
      );
      await page.reload();
      await expect(page.getByText("Available offline")).toBeVisible();
      await expect(frame(page)).toBeVisible();
      await enter(page);
      await page.getByTestId("immersive-next").click();
      await expect(frame(page, 2)).toBeAttached();
      await expect(
        page.frameLocator('iframe[title="Slide 2"]').locator(".star"),
      ).toBeVisible();
      await page.getByTestId("immersive-bookmark").click();
      await expect(page.getByTestId("immersive-bookmark")).toHaveAttribute(
        "aria-pressed",
        "true",
      );
      await expect(
        page.getByTestId("immersive-side-rail-handle"),
      ).toBeVisible();
      await shot(page, info, vp, "offline-immersive");
      await page.keyboard.press("Escape");
      await expect(page.locator(".player.immersive")).toHaveCount(0);
    });
  });
}

// Rotation entry is a touch-device behaviour governed by a per-Learner setting.
test.describe("enter on rotation", () => {
  test.use({
    viewport: { width: 390, height: 844 },
    hasTouch: true,
    isMobile: true,
  });
  test("turning to landscape enters, turning back leaves (setting on)", async ({
    page,
  }) => {
    await openLesson(page);
    await page.setViewportSize({ width: 844, height: 390 });
    await expect(page.locator(".player.immersive")).toBeVisible();
    await expect(
      page.getByRole("status").filter({ hasText: "Immersive mode on" }),
    ).toBeVisible();
    await page.setViewportSize({ width: 390, height: 844 });
    await expect(page.locator(".player.immersive")).toHaveCount(0);
  });
  test("entered by the control, turning back keeps immersive mode (rotated stage)", async ({
    page,
  }) => {
    // Chromium really enters full screen here and then refuses window resizes:
    // keep this scenario on the CSS-only presentation.
    await page.addInitScript(() => {
      Element.prototype.requestFullscreen = () =>
        Promise.reject(new Error("no"));
    });
    await openLesson(page);
    await enter(page);
    await page.setViewportSize({ width: 844, height: 390 });
    await expect(page.locator(".player.immersive")).toBeVisible();
    await page.setViewportSize({ width: 390, height: 844 });
    await expect(page.locator(".player.immersive")).toBeVisible();
    await expect.poll(async () => (await geometry(page)).rotated).toBe(true);
  });
  test("setting off: rotation does nothing, only the control enters", async ({
    page,
    request,
  }) => {
    await setSettings(page, request, { fullscreen_on_rotate: false });
    await openLesson(page);
    await page.setViewportSize({ width: 844, height: 390 });
    await page.waitForTimeout(500);
    await expect(page.locator(".player.immersive")).toHaveCount(0);
    await enter(page);
  });
});

test.describe("desktop never enters on rotation", () => {
  test.use({ viewport: { width: 1280, height: 800 } });
  test("resizing a desktop window does not enter immersive mode", async ({
    page,
  }) => {
    await openLesson(page);
    await page.setViewportSize({ width: 600, height: 400 });
    await page.setViewportSize({ width: 600, height: 900 });
    await page.waitForTimeout(400);
    await expect(page.locator(".player.immersive")).toHaveCount(0);
  });
});
