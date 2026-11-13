import { test, expect, resetTokens } from "./accounts-fixture";
import type { APIRequestContext, Page } from "@playwright/test";
import { apiBase } from "./servers";

// E14-02: identical narration is synthesized and charged once; failed or missing audio
// recovers without charging the Learner again.
// Canvas: S09-Player "Narration unavailable" + "Slide failure" recovery (Retry), D2-Player captions.
resetTokens();
test.afterEach(() => resetTokens());

type Stats = {
  synth_calls: number;
  spent: number;
  characters: number;
  ref_counts: number[];
  regenerations: { status: string }[];
};

async function headers(page: Page) {
  await page.goto("/profile");
  const tokens = await page.evaluate(() =>
    JSON.parse(sessionStorage.getItem("duolify.native-store-fixture")!),
  );
  return { Authorization: `Bearer ${tokens.access_token}` };
}

async function seed(request: APIRequestContext, h: Record<string, string>) {
  const seeded = await request.post(`${apiBase}/test/player/fixture`, {
    headers: h,
  });
  expect(seeded.status()).toBe(201);
  return (await seeded.json()).lesson_id as string;
}

async function statuses(
  request: APIRequestContext,
  h: Record<string, string>,
  lesson: string,
) {
  const manifest = await request.get(
    `${apiBase}/v1/lessons/${lesson}/manifest`,
    {
      headers: h,
    },
  );
  const body = await manifest.json();
  return (body.slides as { narration_status: string }[])
    .map((s) => s.narration_status)
    .join(",");
}

async function stats(request: APIRequestContext, h: Record<string, string>) {
  const r = await request.get(`${apiBase}/test/narration/stats`, {
    headers: h,
  });
  expect(r.status()).toBe(200);
  return (await r.json()) as Stats;
}

for (const width of [1280, 390]) {
  test(`concurrent identical narration synthesizes once and charges once at ${width}px`, async ({
    page,
    request,
  }, testInfo) => {
    await page.setViewportSize({ width, height: 900 });
    const h = await headers(page);
    const lesson = await seed(request, h);
    const twin = await request.post(`${apiBase}/test/narration/twin`, {
      headers: h,
      data: { lesson_id: lesson },
    });
    expect(twin.status()).toBe(201);
    const twinId = (await twin.json()).lesson_id as string;

    const queued = await Promise.all(
      [lesson, twinId].map((lesson_id) =>
        request.post(`${apiBase}/test/narration/queue`, {
          headers: h,
          data: { lesson_id },
        }),
      ),
    );
    expect(queued.map((q) => q.status())).toEqual([202, 202]);
    for (const id of [lesson, twinId])
      await expect
        .poll(() => statuses(request, h, id), { timeout: 30_000 })
        .toBe("ready,ready,ready");

    // Two lessons, three distinct narrations: three provider calls, each object referenced twice.
    const after = await stats(request, h);
    expect(after.synth_calls).toBe(3);
    expect(after.ref_counts).toEqual([2, 2, 2]);
    // One aggregate charge, rounded up once: 50/1440 Credits per new character.
    expect(after.spent).toBe(Math.ceil((after.characters * 50) / 1440));

    await page.goto(`/lesson/${twinId}`);
    await expect(
      page.getByText("Narration is ready. Press Play when you are."),
    ).toBeVisible();
    await page.screenshot({
      path: testInfo.outputPath("narration-deduplicated.png"),
      fullPage: true,
    });
  });
}

test("failed audio keeps the lesson and retries the same narration with one charge", async ({
  page,
  request,
}) => {
  const h = await headers(page);
  const lesson = await seed(request, h);
  const queued = await request.post(`${apiBase}/test/narration/queue`, {
    headers: h,
    data: { lesson_id: lesson, fail: { "sky-2": "alignment" } },
  });
  expect(queued.status()).toBe(202);
  await expect
    .poll(() => statuses(request, h, lesson), { timeout: 30_000 })
    .toBe("ready,failed,ready");
  const failed = await stats(request, h);
  // Only delivered slides were charged.
  expect(failed.spent).toBe(Math.ceil((failed.characters * 50) / 1440));

  await page.goto(`/lesson/${lesson}`);
  await page.getByRole("button", { name: "Next", exact: true }).click();
  await expect(page.getByText("Slide 2 of 3")).toBeVisible();
  await expect(page.getByText("Narration unavailable")).toBeVisible();
  await expect(
    page.getByRole("link", { name: "Take lesson test" }),
  ).toHaveCount(0);

  // The provider recovers; Retry reuses the same content hash.
  await request.post(`${apiBase}/test/narration/faults`, {
    headers: h,
    data: { fail: {} },
  });
  const retry = page.getByRole("button", { name: "Retry narration" });
  await retry.focus();
  await page.keyboard.press("Enter");
  await expect(
    page.getByRole("button", { name: "Play narration", exact: true }),
  ).toBeVisible({ timeout: 30_000 });
  const done = await stats(request, h);
  expect(done.spent).toBe(Math.ceil((done.characters * 50) / 1440));
  expect(done.ref_counts).toEqual([1, 1, 1]);
});

for (const width of [1280, 390]) {
  test(`a missing object recovers from the operator queue without charging at ${width}px`, async ({
    page,
    request,
  }, testInfo) => {
    await page.setViewportSize({ width, height: 900 });
    const h = await headers(page);
    const lesson = await seed(request, h);
    await request.post(`${apiBase}/test/narration/queue`, {
      headers: h,
      data: { lesson_id: lesson },
    });
    await expect
      .poll(() => statuses(request, h, lesson), { timeout: 30_000 })
      .toBe("ready,ready,ready");
    const before = await stats(request, h);

    const lost = await request.post(`${apiBase}/test/narration/lose-object`, {
      headers: h,
      data: { lesson_id: lesson, slide_id: "sky-1" },
    });
    expect(lost.status()).toBe(200);

    await page.goto(`/lesson/${lesson}`);
    await page
      .getByRole("button", { name: "Play narration", exact: true })
      .click();
    await expect(page.getByText("Narration unavailable")).toBeVisible();
    await page.screenshot({
      path: testInfo.outputPath("narration-object-missing.png"),
      fullPage: true,
    });
    const restore = page.getByRole("button", { name: "Restore narration" });
    if (width < 768) {
      await restore.focus();
      await page.keyboard.press("Enter");
    } else await restore.click();
    await expect(
      page.getByText("Narration is being restored. Captions stay available."),
    ).toBeVisible();
    expect((await stats(request, h)).regenerations).toEqual([
      { status: "requested" },
    ]);

    // Operator regeneration is tracked and never charged to the Learner.
    const regenerated = await request.post(
      `${apiBase}/test/narration/operator-regenerate`,
      { headers: h },
    );
    expect(regenerated.status()).toBe(200);
    await page.reload();
    await expect(
      page.getByText("Narration is ready. Press Play when you are."),
    ).toBeVisible();
    await page
      .getByRole("button", { name: "Play narration", exact: true })
      .click();
    await expect(
      page.getByRole("button", { name: "Pause narration", exact: true }),
    ).toBeVisible();
    const after = await stats(request, h);
    expect(after.spent).toBe(before.spent);
    expect(after.regenerations).toEqual([{ status: "completed" }]);
  });
}
