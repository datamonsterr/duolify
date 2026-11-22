// Anchored Quick notes (immersive-mobile-lesson-player 5.1) and the caption/controls overlap
// found in the immersive presentation. Mocked suite: real client build against the
// fixture API, signed in with the native credential-store double.
import { copyFileSync, mkdirSync } from "node:fs";
import { join } from "node:path";
import type {
  APIRequestContext,
  CDPSession,
  Page,
  TestInfo,
} from "@playwright/test";
import { expect, test } from "./native-store-fixture";
import { apiBase, resetAuthRateLimits, webBase } from "./servers";
import type { Tokens } from "../src/features/accounts/secure-store";

const OWNER_SHOTS =
  "/home/dat/.cache/tmp/claude-1000/-home-dat-dev-duolify/3c11ae02-db6a-44d9-9754-88e15e8d834c/scratchpad/anchored-notes-shots";

const VIEWPORTS = [
  { name: "portrait-390x844", width: 390, height: 844 },
  { name: "landscape-844x390", width: 844, height: 390 },
  { name: "desktop-1280x800", width: 1280, height: 800 },
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

type Posted = { url: string; body: Record<string, unknown> };

/** Sign in a fresh Learner, create the fixture Lesson and proxy /v1 to the fixture API. */
async function setup(
  page: Page,
  request: APIRequestContext,
  vp: Vp,
  tag: string,
) {
  await resetAuthRateLimits(request);
  const posted: Posted[] = [];
  const state = { offline: false, lesson: "" };
  await page.route(/\/v1\//, async (route) => {
    const original = route.request();
    if (original.method() === "POST" && original.url().includes("/v1/notes")) {
      if (state.offline) return route.abort("failed");
      posted.push({
        url: original.url(),
        body: JSON.parse(original.postData() ?? "{}"),
      });
    }
    const target = new URL(original.url());
    const headers = original.headers();
    delete headers.origin;
    target.host = new URL(apiBase).host;
    if (target.pathname.startsWith("/v1/lessons/sky/") && state.lesson)
      target.pathname = target.pathname.replace(
        "/lessons/sky/",
        `/lessons/${state.lesson}/`,
      );
    try {
      await route.fulfill({
        response: await request.fetch(target.href, {
          method: original.method(),
          headers,
          data: original.postData() ?? undefined,
        }),
      });
    } catch {
      /* aborted during navigation */
    }
  });
  const email = `anchored-${tag}-${vp.width}-${Date.now()}@example.test`;
  expect(
    (
      await request.post(`${apiBase}/v1/auth/code`, {
        data: { email, age_confirmed: true },
      })
    ).status(),
  ).toBe(202);
  const { code } = await (
    await request.get(`${apiBase}/test/mail?email=${email}`)
  ).json();
  const login = await request.post(`${apiBase}/v1/auth/verify`, {
    data: { email, code, age_confirmed: true },
  });
  expect(login.status()).toBe(200);
  const tokens = (await login.json()) as Tokens;
  const fixture = await request.post(`${apiBase}/test/player/fixture`, {
    headers: { Authorization: `Bearer ${tokens.access_token}` },
  });
  expect(fixture.ok()).toBe(true);
  state.lesson = (await fixture.json()).lesson_id;
  await page.context().addInitScript((val) => {
    sessionStorage.setItem("duolify.native-store-fixture", JSON.stringify(val));
  }, tokens);
  return { posted, state };
}

const enterButton = (page: Page) => page.getByTestId("immersive-enter");
async function openLesson(page: Page, lesson: string, query = "") {
  await page.goto(`${webBase}/lesson/${lesson}${query}`);
  await expect(page.getByText(/Why the sky changes/i).first()).toBeVisible();
}
async function enterImmersive(page: Page) {
  await enterButton(page).click();
  await expect(page.locator(".player.immersive")).toBeVisible();
  await expect(page.getByTestId("immersive-layer")).toBeVisible();
}
async function openRail(page: Page) {
  const handle = page.getByTestId("immersive-side-rail-handle");
  if ((await handle.getAttribute("aria-expanded")) !== "true")
    await handle.click();
}
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
const overlap = (
  a: { x: number; y: number; width: number; height: number },
  b: typeof a,
) =>
  a.x < b.x + b.width &&
  b.x < a.x + a.width &&
  a.y < b.y + b.height &&
  b.y < a.y + a.height;

test.describe("immersive caption never covers the controls (bug from #315)", () => {
  test.use({
    viewport: { width: 844, height: 390 },
    hasTouch: true,
    isMobile: true,
  });

  test("at 844×390 the caption clears the centre buttons while the controls are visible", async ({
    page,
    request,
  }, info) => {
    const vp = VIEWPORTS[1];
    const { state } = await setup(page, request, vp, "caption");
    await openLesson(page, state.lesson);
    await enterImmersive(page);
    await expect(page.getByTestId("immersive-controls")).toHaveAttribute(
      "data-visible",
      "true",
    );
    const caption = page.getByTestId("immersive-caption");
    await expect(caption).toBeVisible();
    // Captions are the Slide's explanation: make it a long one, as real Lessons have.
    await caption.evaluate((el) => {
      el.textContent =
        "Sunlight is scattered by the air, and blue light scatters most, so the sky looks blue by day; near sunset the light travels through more air and the blue is scattered away, leaving orange and red.";
    });
    const cap = (await caption.boundingBox())!;
    const centre = page.locator(".imm-centre .imm-btn");
    expect(await centre.count()).toBeGreaterThanOrEqual(3);
    for (let i = 0; i < (await centre.count()); i++) {
      const b = (await centre.nth(i).boundingBox())!;
      expect(overlap(cap, b), `centre button ${i} under the caption`).toBe(
        false,
      );
    }
    const bottom = (await page.locator(".imm-bottom").boundingBox())!;
    expect(overlap(cap, bottom), "bottom bar under the caption").toBe(false);
    await shot(page, info, vp, "caption-clear-of-controls");
  });
});

for (const vp of VIEWPORTS) {
  test.describe(`anchored note at ${vp.name}`, () => {
    test.use({
      viewport: { width: vp.width, height: vp.height },
      hasTouch: true,
      isMobile: vp.width < 1000,
    });

    test("typed note on a region saves with its anchor; Show in Lesson reopens the moment with the region outlined", async ({
      page,
      request,
    }, info) => {
      const { posted, state } = await setup(page, request, vp, "typed");
      await openLesson(page, state.lesson);
      await page.waitForTimeout(600);
      await enterImmersive(page);
      await openRail(page);
      await shot(page, info, vp, "1-rail-open");

      // "Select area" and the keyboard path: arrows move, Shift+arrows resize, Enter adds the note.
      await page.getByTestId("immersive-select-area").click();
      const box = page.getByTestId("region-box");
      await expect(box).toBeVisible();
      await expect(box).toBeFocused();
      const before = (await box.boundingBox())!;
      await page.keyboard.press("Alt+ArrowRight");
      await page.keyboard.press("Shift+Alt+ArrowDown");
      const after = (await box.boundingBox())!;
      expect(after.width * after.height).toBeGreaterThan(
        before.width * before.height,
      );
      await expect(page.getByTestId("region-handle-se")).toBeVisible();
      await shot(page, info, vp, "2-region-selected");

      // Playback paused when the selection began.
      const t0 = await clock(page);
      await page.waitForTimeout(400);
      expect(await clock(page)).toBe(t0);

      await page.keyboard.press("Enter");
      const text = "Why does the sky turn orange at sunset?";
      const textarea = page.getByRole("textbox", { name: /Your note/i });
      await expect(textarea).toBeVisible();
      await textarea.fill(text);
      await shot(page, info, vp, "3-composer");
      await page.getByRole("button", { name: /Save quick note/i }).click();
      await expect(page.getByText(/Synced to your account/i)).toBeVisible();
      await shot(page, info, vp, "4-saved");

      expect(posted).toHaveLength(1);
      const body = posted[0].body as {
        slide_id: number;
        lesson_id: string;
        anchor: {
          playback_ms: number;
          input: string;
          rect: { x: number; y: number; w: number; h: number };
        };
      };
      expect(body.lesson_id).toBe(state.lesson);
      expect(body.slide_id).toBe(0);
      expect(body.anchor.input).toBe("typed");
      expect(body.anchor.playback_ms).toBeGreaterThanOrEqual(0);
      expect(body.anchor.rect.w).toBeGreaterThan(0.06);
      expect(body.anchor.rect.x + body.anchor.rect.w).toBeLessThanOrEqual(
        1.0001,
      );

      // From the note: Show in Lesson.
      await page.goto(`${webBase}/notes`);
      await page.getByText(text).first().click();
      const show = page.getByTestId("note-show-in-lesson");
      await expect(show).toBeVisible();
      await expect(page.getByTestId("note-anchor-label")).toContainText(
        "Slide 1",
      );
      await shot(page, info, vp, "5-note-with-anchor");
      await show.click();
      await expect(page).toHaveURL(/\/lesson\/.+\?.*region=/);
      await expect(
        page.getByText(/Why the sky changes/i).first(),
      ).toBeVisible();
      const outline = page.getByTestId("region-outline");
      await expect(outline).toBeVisible();
      const arrivedAt = await clock(page);
      expect(Math.abs(arrivedAt - body.anchor.playback_ms)).toBeLessThan(400);
      await page.waitForTimeout(500);
      expect(await clock(page)).toBeLessThan(body.anchor.playback_ms + 1500);
      await shot(page, info, vp, "6-show-in-lesson-framed");
      await enterImmersive(page);
      await expect(page.getByTestId("region-outline")).toBeVisible();
      await shot(page, info, vp, "7-show-in-lesson-immersive");
    });

    test("a note offline queues with its anchor and syncs exactly once", async ({
      page,
      request,
    }, info) => {
      const { posted, state } = await setup(page, request, vp, "offline");
      await openLesson(page, state.lesson);
      await page.waitForTimeout(500);
      await page
        .getByRole("button", { name: "Quick note", exact: true })
        .click();
      await page
        .getByRole("textbox", { name: /Your note/i })
        .fill("Offline anchored note");
      state.offline = true;
      await page.getByRole("button", { name: /Save quick note/i }).click();
      await expect(
        page.getByText(/Offline variant · saved on this device/i),
      ).toBeVisible();
      const queued = await page.evaluate(() =>
        JSON.parse(localStorage.getItem("duolify.notes.queue") ?? "[]"),
      );
      expect(queued).toHaveLength(1);
      expect(queued[0].anchor).toMatchObject({ input: "typed" });
      expect(queued[0].anchor.playbackMs).toBeGreaterThanOrEqual(0);
      expect(posted).toHaveLength(0);
      await shot(page, info, vp, "offline-queued");
      state.offline = false;
      await page
        .getByRole("button", { name: /Sync now|Synchronize|Retry sync/i })
        .click();
      await expect(page.getByText(/Synced to your account/i)).toBeVisible();
      expect(posted).toHaveLength(1);
      expect((posted[0].body.anchor as { input: string }).input).toBe("typed");
    });
  });
}

test.describe("region selection by two-finger spread", () => {
  test.use({
    viewport: { width: 844, height: 390 },
    hasTouch: true,
    isMobile: true,
  });

  async function spread(
    page: Page,
    cdp: CDPSession,
    a: { x: number; y: number },
    b: { x: number; y: number },
  ) {
    const mid = { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 };
    await cdp.send("Input.dispatchTouchEvent", {
      type: "touchStart",
      touchPoints: [mid, { x: mid.x + 1, y: mid.y + 1 }],
    });
    for (let i = 1; i <= 6; i++)
      await cdp.send("Input.dispatchTouchEvent", {
        type: "touchMove",
        touchPoints: [
          {
            x: mid.x + ((a.x - mid.x) * i) / 6,
            y: mid.y + ((a.y - mid.y) * i) / 6,
          },
          {
            x: mid.x + ((b.x - mid.x) * i) / 6,
            y: mid.y + ((b.y - mid.y) * i) / 6,
          },
        ],
      });
    await cdp.send("Input.dispatchTouchEvent", {
      type: "touchEnd",
      touchPoints: [],
    });
  }

  test("a spread selects and pauses; a tiny one is discarded", async ({
    page,
    request,
  }, info) => {
    const vp = VIEWPORTS[1];
    const { state } = await setup(page, request, vp, "spread");
    await openLesson(page, state.lesson);
    await page.waitForTimeout(600);
    await enterImmersive(page);
    const cdp = await page.context().newCDPSession(page);
    const a = await stagePoint(page, 0.25, 0.3);
    const b = await stagePoint(page, 0.6, 0.7);
    await spread(page, cdp, a, b);
    await expect(page.getByTestId("region-box")).toBeVisible();
    const t0 = await clock(page);
    await page.waitForTimeout(400);
    expect(await clock(page)).toBe(t0);
    await shot(page, info, vp, "spread-selected");
    // Handles adjust it.
    const se = page.getByTestId("region-handle-se");
    const h = (await se.boundingBox())!;
    const before = (await page.getByTestId("region-box").boundingBox())!;
    await page.mouse.move(h.x + h.width / 2, h.y + h.height / 2);
    await page.mouse.down();
    await page.mouse.move(h.x + h.width / 2 + 30, h.y + h.height / 2 + 20, {
      steps: 4,
    });
    await page.mouse.up();
    const after = (await page.getByTestId("region-box").boundingBox())!;
    expect(after.width).toBeGreaterThan(before.width);
    await page.getByTestId("region-clear").click();
    await expect(page.getByTestId("region-box")).toHaveCount(0);

    // Accidental: two fingers a few pixels apart select nothing.
    const c = await stagePoint(page, 0.5, 0.2);
    await spread(
      page,
      cdp,
      { x: c.x - 3, y: c.y - 3 },
      { x: c.x + 3, y: c.y + 3 },
    );
    await expect(page.getByTestId("region-box")).toHaveCount(0);
    await expect(page.getByTestId("immersive-notes-announcer")).toContainText(
      "too small",
    );
  });
});
