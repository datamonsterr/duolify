import { test, expect, resetTokens } from "./accounts-fixture";
import type { APIRequestContext, Page } from "@playwright/test";
import { apiBase } from "./servers";

// E17-08: pinned speakers are narrated with allowlisted Character voices through the E14 pipeline.
// Canvas: S09-Player (narration ready / unavailable states, captions, seek). Two voices alternate in
// speaker order, replay is free, a retired voice only changes new audio, entitlement and the TTS
// outage keep captions readable.
resetTokens();
test.afterEach(async ({ request }) => {
  resetTokens();
  await request.post(`${apiBase}/test/characters/retire-voices`, {
    data: { keys: [] },
  });
});

type Headers = Record<string, string>;
type Stats = { synth_calls: number; spent: number };
type Row = {
  slide_id: string;
  index: number;
  speaker_key: string;
  actual_voice: string;
  requested_voice: string;
  fallback_reason: string;
  status: string;
  failure_code: string;
  hash: string;
  voice_config_version: string;
};
type Segments = {
  segments: Row[];
  character_create_jobs: number;
  voices_used: string[];
  timeouts_ms: number[];
};

async function headers(page: Page): Promise<Headers> {
  await page.goto("/profile");
  const tokens = await page.evaluate(() =>
    JSON.parse(sessionStorage.getItem("duolify.native-store-fixture")!),
  );
  return { Authorization: `Bearer ${tokens.access_token}` };
}

const stats = async (request: APIRequestContext, h: Headers, salt: string) =>
  (await (
    await request.get(`${apiBase}/test/narration/stats?salt=${salt}`, {
      headers: h,
    })
  ).json()) as Stats;

const segments = async (
  request: APIRequestContext,
  h: Headers,
  lesson: string,
) =>
  (await (
    await request.get(
      `${apiBase}/test/characters/segments?lesson_id=${lesson}`,
      {
        headers: h,
      },
    )
  ).json()) as Segments;

async function narrated(
  request: APIRequestContext,
  h: Headers,
  salt: string,
  options: { fault?: string; twinOf?: string } = {},
) {
  const seeded = await request.post(
    `${apiBase}/test/characters/narrated-lesson`,
    {
      headers: h,
      data: { salt, fault: options.fault ?? "", twin_of: options.twinOf ?? "" },
    },
  );
  expect(seeded.status()).toBe(201);
  const lesson = (await seeded.json()).lesson_id as string;
  expect(
    (
      await request.post(`${apiBase}/test/narration/salt`, {
        headers: h,
        data: { lesson_id: lesson, salt },
      })
    ).status(),
  ).toBe(204);
  expect(
    (
      await request.post(`${apiBase}/test/narration/queue`, {
        headers: h,
        data: { lesson_id: lesson, salt, fail: {} },
      })
    ).status(),
  ).toBe(202);
  await expect
    .poll(
      async () =>
        (await segments(request, h, lesson)).segments.filter(
          (s) => s.status !== "pending",
        ).length,
      { timeout: 40_000 },
    )
    .toBe(4);
  // One River run per Lesson: wait until the slide-level slide is settled too.
  await expect
    .poll(
      async () =>
        (
          await (
            await request.get(`${apiBase}/v1/lessons/${lesson}/manifest`, {
              headers: h,
            })
          ).json()
        ).slides
          .map((s: { narration_status: string }) => s.narration_status)
          .includes("pending"),
      { timeout: 40_000 },
    )
    .toBe(false);
  return lesson;
}

const salt = () =>
  Math.random().toString(36).slice(2).padEnd(8, "x").slice(0, 8);

for (const width of [1280, 390]) {
  test(`two Character voices alternate, replay costs nothing, seek stays aligned and a retired voice only changes new audio at ${width}px`, async ({
    page,
    request,
  }, testInfo) => {
    await page.setViewportSize({ width, height: 900 });
    await page.emulateMedia({
      colorScheme: width === 390 ? "dark" : "light",
      reducedMotion: width === 390 ? "reduce" : "no-preference",
    });
    await page.addInitScript(() => {
      const w = window as unknown as { __audioPlays: number };
      w.__audioPlays = 0;
      const play = HTMLMediaElement.prototype.play;
      HTMLMediaElement.prototype.play = function (...args) {
        w.__audioPlays += 1;
        return play.apply(this, args);
      };
    });
    const h = await headers(page);
    const funded = await request.post(`${apiBase}/test/subscription/fund`, {
      headers: h,
      data: { plan_id: "plus" },
    });
    expect(funded.ok()).toBe(true);
    const s = salt();
    const lesson = await narrated(request, h, s);

    // Two approved voices, in speaker order; no Character creation Credits are involved.
    const first = await segments(request, h, lesson);
    expect(
      first.segments.map((r) => `${r.slide_id}:${r.index}:${r.actual_voice}`),
    ).toEqual([
      "sky-1:0:voice-provisional-guide",
      "sky-1:1:voice-provisional-supporter",
      "sky-1:2:voice-provisional-guide",
      "sky-2:0:voice-provisional-supporter",
    ]);
    expect(first.segments.every((r) => r.fallback_reason === "")).toBe(true);
    expect(first.segments[0].hash).toBe(first.segments[2].hash);
    expect(first.segments[1].hash).toBe(first.segments[3].hash);
    expect(first.segments[0].hash).not.toBe(first.segments[1].hash);
    expect(first.character_create_jobs).toBe(0);
    expect(Math.max(...first.timeouts_ms)).toBeLessThanOrEqual(30_000);
    const afterSynthesis = await stats(request, h, s);
    expect(afterSynthesis.spent).toBeGreaterThan(0);
    // Identical dialogue is synthesized once per distinct text and voice (plus the slide-level slide).
    expect(afterSynthesis.synth_calls).toBe(3);

    await page.goto(`/lesson/${lesson}`);
    await expect(page.getByText("Slide 1 of 3")).toBeVisible();
    await expect(
      page.getByText("Narration is ready. Press Play when you are."),
    ).toBeVisible();
    const items = page.locator(".narration-segment");
    await expect(items).toHaveCount(3);
    await expect(items.nth(0)).toHaveAttribute(
      "data-voice",
      "voice-provisional-guide",
    );
    await expect(items.nth(1)).toHaveAttribute(
      "data-voice",
      "voice-provisional-supporter",
    );
    expect(
      await page.evaluate(
        () => (window as unknown as { __audioPlays: number }).__audioPlays,
      ),
    ).toBe(0);
    await page.screenshot({
      path: testInfo.outputPath("cast-narration-ready.png"),
      fullPage: true,
    });

    const play = page.getByRole("button", {
      name: "Play narration",
      exact: true,
    });
    if (width < 768) {
      await play.focus();
      await page.keyboard.press("Enter");
    } else await play.click();
    await expect(
      page.getByRole("button", { name: "Pause narration", exact: true }),
    ).toBeVisible();
    await expect(items.nth(0)).toHaveAttribute("aria-current", "true");
    // The supporter takes over when the guide's segment ends: audio alternates in order.
    await expect(items.nth(1)).toHaveAttribute("aria-current", "true", {
      timeout: 15_000,
    });
    await expect
      .poll(() =>
        page.evaluate(
          () => (window as unknown as { __audioPlays: number }).__audioPlays,
        ),
      )
      .toBeGreaterThanOrEqual(2);
    await page.screenshot({
      path: testInfo.outputPath("cast-narration-playing.png"),
      fullPage: true,
    });

    // Aligned seek: moving the clock into the first segment returns to the guide.
    await page.getByRole("button", { name: "Pause narration" }).click();
    const time = page.getByRole("slider", { name: "Slide time" });
    await time.fill("400");
    await page.getByRole("button", { name: "Play narration" }).click();
    await expect(items.nth(0)).toHaveAttribute("aria-current", "true");

    // Replay and cache: segment audio and alignment are in the offline cache; nothing is synthesized or charged.
    await expect
      .poll(() =>
        page.evaluate(async () => {
          const cache = await caches.open("duolify-narration-v1");
          return (await cache.keys()).filter((r) => r.url.includes("__s"))
            .length;
        }),
      )
      .toBeGreaterThanOrEqual(4);
    const pause = page.getByRole("button", { name: "Pause narration" });
    if (await pause.isVisible()) await pause.click();
    const replayed = await stats(request, h, s);
    expect(replayed).toEqual(afterSynthesis);

    // Retire the guide voice: new audio resolves the safe default; stored audio is unchanged.
    expect(
      (
        await request.post(`${apiBase}/test/characters/retire-voices`, {
          data: { keys: ["voice-provisional-guide"] },
        })
      ).status(),
    ).toBe(204);
    const lesson2 = await narrated(request, h, s, { twinOf: lesson });
    const second = await segments(request, h, lesson2);
    expect(second.segments[0]).toMatchObject({
      actual_voice: "default",
      fallback_reason: "voice_retired",
    });
    expect(second.segments[0].voice_config_version).toContain("+retired");
    expect(second.segments[1]).toMatchObject({
      actual_voice: "voice-provisional-supporter",
      fallback_reason: "",
    });
    const unchanged = await segments(request, h, lesson);
    expect(unchanged.segments).toEqual(first.segments);
    // The supporter's stored audio is reused for free; only the guide's new default-voice audio is synthesized.
    expect(second.segments[1].hash).toBe(first.segments[1].hash);
    expect(second.segments[0].hash).not.toBe(first.segments[0].hash);
    expect((await stats(request, h, s)).synth_calls).toBe(
      afterSynthesis.synth_calls + 1,
    );
    expect(second.character_create_jobs).toBe(0);
    await page.goto(`/lesson/${lesson2}`);
    await expect(page.locator(".narration-segment").first()).toHaveAttribute(
      "data-voice",
      "default",
    );
  });
}

test("a Learner without the curated-voice plan hears the default voice, never auto-charged for a later plan", async ({
  page,
  request,
}) => {
  const h = await headers(page);
  const s = salt();
  const lesson = await narrated(request, h, s);
  const rows = (await segments(request, h, lesson)).segments;
  expect(rows.every((r) => r.actual_voice === "default")).toBe(true);
  expect(rows.every((r) => r.fallback_reason === "voice_not_entitled")).toBe(
    true,
  );
  const before = await stats(request, h, s);
  // Buying the plan afterwards changes only future audio: delivered content is never re-charged.
  const funded = await request.post(`${apiBase}/test/subscription/fund`, {
    headers: h,
    data: { plan_id: "plus" },
  });
  expect(funded.ok()).toBe(true);
  await page.goto(`/lesson/${lesson}`);
  await expect(page.locator(".narration-segment")).toHaveCount(3);
  expect(await stats(request, h, s)).toEqual(before);
  expect(
    (await segments(request, h, lesson)).segments.map((r) => r.actual_voice),
  ).toEqual(rows.map((r) => r.actual_voice));
});

test("a TTS outage keeps captions readable and charges nothing for the failed audio", async ({
  page,
  request,
}) => {
  await page.setViewportSize({ width: 390, height: 900 });
  const h = await headers(page);
  await request.post(`${apiBase}/test/subscription/fund`, {
    headers: h,
    data: { plan_id: "plus" },
  });
  const s = salt();
  const lesson = await narrated(request, h, s, { fault: "engine" });
  const result = await segments(request, h, lesson);
  expect(result.segments.map((r) => `${r.speaker_key}:${r.status}`)).toEqual([
    "guide:ready",
    "supporter:failed",
    "guide:ready",
    "supporter:failed",
  ]);
  expect(result.segments[1].failure_code).toBe("engine_unavailable");
  expect(result.character_create_jobs).toBe(0);
  await page.goto(`/lesson/${lesson}`);
  await expect(page.getByText("Slide 1 of 3")).toBeVisible();
  // Mixed slide: the guide still speaks, the supporter's line is captions only.
  await expect(page.getByText("Some lines have captions only.")).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Play narration", exact: true }),
  ).toBeVisible();
  await expect(page.locator(".narration-segment").nth(1)).toHaveAttribute(
    "data-state",
    "failed",
  );
  await page.getByRole("button", { name: "Next", exact: true }).click();
  await expect(page.getByText("Slide 2 of 3")).toBeVisible();
  await expect(page.getByText("Narration unavailable")).toBeVisible();
  await expect(
    page.getByText(`Earth turns every day ${s}.`).first(),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Retry narration" }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Play narration" }),
  ).toHaveCount(0);
  await page.getByRole("button", { name: "Next", exact: true }).click();
  await expect(
    page.getByRole("link", { name: "Take lesson test" }),
  ).toBeVisible();
});
