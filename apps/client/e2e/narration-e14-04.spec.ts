import { test, expect, resetTokens } from "./accounts-fixture";
import type { APIRequestContext, Page } from "@playwright/test";
import { apiBase } from "./servers";

// E14-04: bounded ElevenLabs synthesis (fake vendor). A vendor outage degrades to captions only,
// charges nothing for undelivered audio and recovers with Retry.
// Canvas: S09-Player "Narration unavailable" + captions, D2-Player captions.
resetTokens();
test.afterEach(() => resetTokens());

async function headers(page: Page) {
  await page.goto("/profile");
  const tokens = await page.evaluate(() =>
    JSON.parse(sessionStorage.getItem("duolify.native-store-fixture")!),
  );
  return { Authorization: `Bearer ${tokens.access_token}` };
}

async function statuses(
  request: APIRequestContext,
  h: Record<string, string>,
  lesson: string,
) {
  const manifest = await request.get(
    `${apiBase}/v1/lessons/${lesson}/manifest`,
    { headers: h },
  );
  const body = await manifest.json();
  return (body.slides as { narration_status: string }[])
    .map((s) => s.narration_status)
    .join(",");
}

async function stats(
  request: APIRequestContext,
  h: Record<string, string>,
  salt: string,
) {
  const r = await request.get(`${apiBase}/test/narration/stats?salt=${salt}`, {
    headers: h,
  });
  expect(r.status()).toBe(200);
  return (await r.json()) as { spent: number; characters: number };
}

async function start(
  page: Page,
  request: APIRequestContext,
  fail: Record<string, string>,
) {
  const h = await headers(page);
  const seeded = await request.post(`${apiBase}/test/player/fixture`, {
    headers: h,
  });
  expect(seeded.status()).toBe(201);
  const lesson = (await seeded.json()).lesson_id as string;
  const salt = Math.random().toString(36).slice(2).padEnd(8, "x").slice(0, 8);
  const salted = await request.post(`${apiBase}/test/narration/salt`, {
    headers: h,
    data: { lesson_id: lesson, salt },
  });
  expect(salted.status()).toBe(204);
  const queued = await request.post(`${apiBase}/test/narration/queue`, {
    headers: h,
    data: { lesson_id: lesson, salt, fail },
  });
  expect(queued.status()).toBe(202);
  return { h, lesson, salt };
}

for (const width of [1280, 390]) {
  test(`a vendor outage on one slide falls back to captions and charges only delivered audio at ${width}px`, async ({
    page,
    request,
  }, testInfo) => {
    await page.setViewportSize({ width, height: 900 });
    const { h, lesson, salt } = await start(page, request, {
      "sky-2": "vendor_outage",
    });
    await expect
      .poll(() => statuses(request, h, lesson), { timeout: 30_000 })
      .toBe("ready,failed,ready");
    const after = await stats(request, h, salt);
    expect(after.spent).toBe(Math.ceil((after.characters * 50) / 1440));

    await page.goto(`/lesson/${lesson}`);
    await page.getByRole("button", { name: "Next", exact: true }).click();
    await expect(page.getByText("Slide 2 of 3")).toBeVisible();
    await expect(page.getByText("Narration unavailable")).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Next", exact: true }),
    ).toBeEnabled();
    await page.screenshot({
      path: testInfo.outputPath("narration-vendor-outage.png"),
      fullPage: true,
    });

    // The vendor recovers; keyboard Retry reuses the same narration.
    await request.post(`${apiBase}/test/narration/faults`, {
      headers: h,
      data: { fail: {}, salt },
    });
    const retry = page.getByRole("button", { name: "Retry narration" });
    await retry.focus();
    await page.keyboard.press("Enter");
    await expect(
      page.getByRole("button", { name: "Play narration", exact: true }),
    ).toBeVisible({ timeout: 30_000 });
  });
}

test("a full vendor outage releases the reservation and keeps the lesson playable", async ({
  page,
  request,
}) => {
  const { h, lesson, salt } = await start(page, request, {
    "sky-1": "vendor_outage",
    "sky-2": "vendor_outage",
    "sky-3": "vendor_outage",
  });
  await expect
    .poll(() => statuses(request, h, lesson), { timeout: 30_000 })
    .toBe("failed,failed,failed");
  expect((await stats(request, h, salt)).spent).toBe(0);
  await page.goto(`/lesson/${lesson}`);
  await expect(page.getByText("Narration unavailable")).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Next", exact: true }),
  ).toBeEnabled();
});
