// Shared helpers for the immersive note and question specs: sign in a fresh Learner, open the
// fixture Lesson, enter immersive mode, stage geometry. Not a spec.
import { copyFileSync, mkdirSync } from "node:fs";
import { join } from "node:path";
import type { APIRequestContext, Page, TestInfo } from "@playwright/test";
import { expect } from "./native-store-fixture";
import { apiBase, resetAuthRateLimits, webBase } from "./servers";
import type { Tokens } from "../src/features/accounts/secure-store";

export const OWNER_SHOTS =
  "/home/dat/.cache/tmp/claude-1000/-home-dat-dev-duolify/3c11ae02-db6a-44d9-9754-88e15e8d834c/scratchpad/anchored-notes-shots";

export const VIEWPORTS = [
  { name: "portrait-390x844", width: 390, height: 844 },
  { name: "landscape-844x390", width: 844, height: 390 },
  { name: "desktop-1280x800", width: 1280, height: 800 },
] as const;
export type Vp = (typeof VIEWPORTS)[number];

export async function shot(
  page: Page,
  info: TestInfo,
  vp: Vp,
  name: string,
  dir = OWNER_SHOTS,
) {
  const file = info.outputPath(`${vp.name}-${name}.png`);
  await page.screenshot({ path: file });
  try {
    mkdirSync(dir, { recursive: true });
    copyFileSync(file, join(dir, `${vp.name}-${name}.png`));
  } catch {
    /* owner copy is best effort (CI has no scratchpad) */
  }
}

export type Posted = { url: string; body: Record<string, unknown> };

/** Sign in a fresh Learner, create the fixture Lesson and proxy /v1 to the fixture API. */
export async function setup(
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

export const enterButton = (page: Page) => page.getByTestId("immersive-enter");
export async function openLesson(page: Page, lesson: string, query = "") {
  await page.goto(`${webBase}/lesson/${lesson}${query}`);
  await expect(page.getByText(/Why the sky changes/i).first()).toBeVisible();
}
export async function enterImmersive(page: Page) {
  await enterButton(page).click();
  await expect(page.locator(".player.immersive")).toBeVisible();
  await expect(page.getByTestId("immersive-layer")).toBeVisible();
}
export async function openRail(page: Page) {
  const handle = page.getByTestId("immersive-side-rail-handle");
  if ((await handle.getAttribute("aria-expanded")) !== "true")
    await handle.click();
}
export const clock = (page: Page) =>
  page.evaluate(() =>
    Number(
      (
        document.querySelector(
          'input[aria-label="Slide time"]',
        ) as HTMLInputElement
      ).value,
    ),
  );

export async function geometry(page: Page) {
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
export async function stagePoint(page: Page, nx: number, ny: number) {
  const g = await geometry(page);
  const sx = (nx - 0.5) * 1280 * g.scale;
  const sy = (ny - 0.5) * 720 * g.scale;
  return g.rotated
    ? { x: g.w / 2 - sy, y: g.h / 2 + sx }
    : { x: g.w / 2 + sx, y: g.h / 2 + sy };
}
export const overlap = (
  a: { x: number; y: number; width: number; height: number },
  b: typeof a,
) =>
  a.x < b.x + b.width &&
  b.x < a.x + a.width &&
  a.y < b.y + b.height &&
  b.y < a.y + a.height;

/** A toast must never sit over the player's title bar: their boxes do not intersect. */
export async function expectToastClearOfTitle(page: Page) {
  const toast = await page.getByTestId("toast").boundingBox();
  const title = await page.locator(".imm-title").first().boundingBox();
  expect(toast && title).toBeTruthy();
  const t = toast!;
  const b = title!;
  const apart =
    t.x >= b.x + b.width ||
    b.x >= t.x + t.width ||
    t.y >= b.y + b.height ||
    b.y >= t.y + t.height;
  expect(apart, "toast overlaps the title bar").toBe(true);
}
