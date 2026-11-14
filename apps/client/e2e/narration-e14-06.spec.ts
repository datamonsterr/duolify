import { test, expect, resetTokens } from "./accounts-fixture";
import type { APIRequestContext, Page } from "@playwright/test";
import { apiBase } from "./servers";

// E14-06: Plus/Pro choose a curated voice only after seeing the new character quote; declining
// changes and charges nothing, the old narration replays free, and insufficient audio credits
// disable only audio. Canvas: S09-Player (voice sheet over the player, "Narration unavailable"
// recovery), S17-Shop (plans with curated voices), S04-Create (quote-before-spend pattern).
resetTokens();
test.afterEach(() => resetTokens());

type Stats = { spent: number; characters: number; ref_counts: number[] };
type Headers = Record<string, string>;

async function headers(page: Page): Promise<Headers> {
  await page.goto("/profile");
  const tokens = await page.evaluate(() =>
    JSON.parse(sessionStorage.getItem("duolify.native-store-fixture")!),
  );
  return { Authorization: `Bearer ${tokens.access_token}` };
}

async function stats(request: APIRequestContext, h: Headers, salt: string) {
  const r = await request.get(`${apiBase}/test/narration/stats?salt=${salt}`, {
    headers: h,
  });
  expect(r.status()).toBe(200);
  return (await r.json()) as Stats;
}

async function statuses(
  request: APIRequestContext,
  h: Headers,
  lesson: string,
) {
  const body = await (
    await request.get(`${apiBase}/v1/lessons/${lesson}/manifest`, {
      headers: h,
    })
  ).json();
  return (body.slides as { narration_status: string }[])
    .map((s) => s.narration_status)
    .join(",");
}

async function start(
  page: Page,
  request: APIRequestContext,
  options: { noTopup?: boolean } = {},
) {
  const h = await headers(page);
  const seeded = await request.post(`${apiBase}/test/player/fixture`, {
    headers: h,
  });
  expect(seeded.status()).toBe(201);
  const lesson = (await seeded.json()).lesson_id as string;
  const salt = Math.random().toString(36).slice(2).padEnd(8, "x").slice(0, 8);
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
        data: {
          lesson_id: lesson,
          salt,
          fail: {},
          no_topup: !!options.noTopup,
        },
      })
    ).status(),
  ).toBe(202);
  return { h, lesson, salt };
}

async function fund(request: APIRequestContext, h: Headers, plan: string) {
  const res = await request.post(`${apiBase}/test/subscription/fund`, {
    headers: h,
    data: { plan_id: plan },
  });
  expect(res.ok()).toBe(true);
}

async function selectedVoice(request: APIRequestContext, h: Headers) {
  const res = await request.get(`${apiBase}/v1/me/narration/voices`, {
    headers: h,
  });
  expect(res.status()).toBe(200);
  return (await res.json()) as {
    selected: string;
    entitled: boolean;
    upsell_allowed: boolean;
  };
}

for (const width of [1280, 390]) {
  test(`a Plus Learner changes voice, declines the quote and replays the old narration, then confirms at ${width}px`, async ({
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
    const { h, lesson, salt } = await start(page, request);
    await expect
      .poll(() => statuses(request, h, lesson), { timeout: 30_000 })
      .toBe("ready,ready,ready");
    await fund(request, h, "plus");
    const before = await stats(request, h, salt);
    expect(before.characters).toBeGreaterThan(0);
    expect(await selectedVoice(request, h)).toMatchObject({
      selected: "default",
      entitled: true,
      upsell_allowed: false,
    });

    await page.goto(`/lesson/${lesson}`);
    await expect(page.getByText("Slide 1 of 3")).toBeVisible();

    // Open the voice sheet with the keyboard and pick a curated voice with the keyboard.
    const voiceButton = page.getByRole("button", {
      name: "Voice",
      exact: true,
    });
    await voiceButton.focus();
    await page.keyboard.press("Enter");
    await expect(
      page.getByRole("group", { name: "Narration voice" }),
    ).toBeVisible();
    await expect(page.getByLabel("Default voice")).toBeChecked();
    await page.getByLabel("Warm (test voice)").focus();
    await page.keyboard.press("Space");

    // The quote names the new characters and credits and says nothing is charged yet.
    const credits = Math.ceil((before.characters * 50) / 1440);
    const quote = page.getByRole("region", { name: "Voice quote" });
    await expect(quote).toContainText(
      `needs ${before.characters} new characters`,
    );
    await expect(quote).toContainText(`${credits} credits`);
    await expect(quote).toContainText("Nothing is charged until you confirm");
    await page.screenshot({
      path: testInfo.outputPath(`voice-quote-${width}.png`),
      fullPage: true,
    });
    expect((await stats(request, h, salt)).spent).toBe(before.spent);

    // Decline: no charge, no setting change, no new asset.
    const decline = page.getByRole("button", { name: "Keep current voice" });
    await decline.focus();
    await page.keyboard.press("Enter");
    await expect(
      page.getByText("Kept Default voice. Nothing was charged."),
    ).toBeVisible();
    expect(await stats(request, h, salt)).toEqual(before);
    expect((await selectedVoice(request, h)).selected).toBe("default");

    // The old narration still replays and is free.
    await page.getByRole("button", { name: "Close Voice" }).click();
    await page
      .getByRole("button", { name: "Play narration", exact: true })
      .click();
    await expect(
      page.getByRole("button", { name: "Pause narration", exact: true }),
    ).toBeVisible();
    await expect
      .poll(() =>
        page.evaluate(
          () => (window as unknown as { __audioPlays: number }).__audioPlays,
        ),
      )
      .toBeGreaterThan(0);
    expect((await stats(request, h, salt)).spent).toBe(before.spent);

    // Confirm: now (and only now) the new voice is synthesized within the quote; the old asset stays.
    await page.getByRole("button", { name: "Voice", exact: true }).click();
    await page.getByLabel("Warm (test voice)").click();
    await page.getByRole("button", { name: "Confirm and narrate" }).click();
    await expect(
      page.getByText(
        /Warm \(test voice\) is ready\. Press Play when you are\./,
      ),
    ).toBeVisible({
      timeout: 30_000,
    });
    const after = await stats(request, h, salt);
    expect(after.spent).toBe(before.spent + credits);
    expect(after.ref_counts.length).toBe(before.ref_counts.length * 2);
    expect(after.ref_counts.every((n) => n >= 1)).toBe(true);
    expect((await selectedVoice(request, h)).selected).toBe("fixture-warm");

    // Switching back to the stored voice is free.
    await page.getByLabel("Default voice").click();
    await expect(
      page.getByRole("region", { name: "Voice quote" }),
    ).toContainText("Switching is free");
    await page.getByRole("button", { name: "Use this voice" }).click();
    await expect(page.getByText(/Default voice is ready/)).toBeVisible({
      timeout: 30_000,
    });
    expect((await stats(request, h, salt)).spent).toBe(after.spent);
    expect((await selectedVoice(request, h)).selected).toBe("default");
  });
}

test("a Learner without curated voices sees them locked with one upgrade prompt, a Learn Learner sees none", async ({
  page,
  request,
}) => {
  const { h, lesson } = await start(page, request);
  await expect
    .poll(() => statuses(request, h, lesson), { timeout: 30_000 })
    .toBe("ready,ready,ready");
  expect(await selectedVoice(request, h)).toMatchObject({
    entitled: false,
    upsell_allowed: true,
  });
  await page.goto(`/lesson/${lesson}`);
  await page.getByRole("button", { name: "Voice", exact: true }).click();
  await expect(page.getByLabel(/Warm \(test voice\)/)).toBeDisabled();
  await expect(
    page.getByText(/Curated voices come with Plus and Pro/),
  ).toBeVisible();
  await expect(page.getByRole("link", { name: "See plans" })).toBeVisible();

  // Learn includes "No upsell banners" but not curated voices: locked, no upgrade guidance.
  await fund(request, h, "learn");
  expect(await selectedVoice(request, h)).toMatchObject({
    entitled: false,
    upsell_allowed: false,
  });
  await page.reload();
  await page.getByRole("button", { name: "Voice", exact: true }).click();
  await expect(page.getByLabel(/Warm \(test voice\)/)).toBeDisabled();
  await expect(page.getByText(/Curated voices come with/)).toHaveCount(0);

  // The server refuses a curated voice without the verified entitlement.
  const refused = await request.post(
    `${apiBase}/v1/lessons/${lesson}/narration/voice/quote`,
    {
      headers: h,
      data: { voice_key: "fixture-warm" },
    },
  );
  expect(refused.status()).toBe(403);
  expect(await refused.json()).toMatchObject({ code: "voice_not_entitled" });
});

test("insufficient audio credits disable only audio and recover without blocking text or the test", async ({
  page,
  request,
}, testInfo) => {
  await page.setViewportSize({ width: 390, height: 900 });
  const { h, lesson } = await start(page, request, { noTopup: true });
  await expect
    .poll(() => statuses(request, h, lesson), { timeout: 30_000 })
    .toBe("disabled,disabled,disabled");
  await page.goto(`/lesson/${lesson}`);
  await expect(page.getByText("Slide 1 of 3")).toBeVisible();
  await expect(page.getByText("Narration paused")).toBeVisible();
  await expect(
    page.getByText(/The lesson text, captions and test still work/),
  ).toBeVisible();
  await expect(page.getByRole("link", { name: "Add credits" })).toBeVisible();
  await page.screenshot({
    path: testInfo.outputPath("audio-credits-paused.png"),
    fullPage: true,
  });
  // Text and the lesson test stay reachable.
  await expect(
    page.getByRole("button", { name: "Next", exact: true }),
  ).toBeEnabled();
  await page.getByRole("button", { name: "Next", exact: true }).click();
  await expect(page.getByText("Slide 2 of 3")).toBeVisible();

  // Credits arrive; keyboard Retry narrates without any further consent beyond the existing Retry.
  expect(
    (
      await request.post(`${apiBase}/test/credits/topup`, { headers: h })
    ).status(),
  ).toBe(204);
  const retry = page.getByRole("button", { name: "Retry narration" });
  await retry.focus();
  await page.keyboard.press("Enter");
  await expect(
    page.getByRole("button", { name: "Play narration", exact: true }),
  ).toBeVisible({ timeout: 30_000 });
});
