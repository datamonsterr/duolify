import { test, expect, resetTokens } from "./accounts-fixture";
import { apiBase } from "./servers";

// E14-01: queue mock narration for a generated lesson, then explicitly play it.
// Canvas: S09-Player "Idle — narration ready" and "Muted · audio unavailable".
resetTokens();
test.afterEach(() => resetTokens());

type Slide = { slide_id: string; narration_status: string };

for (const width of [1280, 390]) {
  test(`explicit Play starts one ready mock narration with captions at ${width}px`, async ({
    page,
    request,
  }, testInfo) => {
    await page.setViewportSize({ width, height: 900 });
    // Count real media starts: narration must never autoplay.
    await page.addInitScript(() => {
      const w = window as unknown as { __played: number };
      w.__played = 0;
      const play = HTMLMediaElement.prototype.play;
      HTMLMediaElement.prototype.play = function (...args) {
        w.__played += 1;
        return play.apply(this, args);
      };
    });
    await page.goto("/profile");
    const tokens = await page.evaluate(() =>
      JSON.parse(sessionStorage.getItem("duolify.native-store-fixture")!),
    );
    const headers = { Authorization: `Bearer ${tokens.access_token}` };
    const seeded = await request.post(`${apiBase}/test/player/fixture`, {
      headers,
    });
    expect(seeded.status()).toBe(201);
    const { lesson_id } = await seeded.json();
    const queued = await request.post(`${apiBase}/test/narration/queue`, {
      headers,
      data: {
        lesson_id,
        // sky-2 returns a misaligned result, sky-3 a provider failure.
        fail: { "sky-2": "alignment", "sky-3": "engine" },
      },
    });
    expect(queued.status()).toBe(202);
    await expect
      .poll(async () => {
        const body = await (
          await request.get(`${apiBase}/v1/lessons/${lesson_id}/manifest`, {
            headers,
          })
        ).json();
        return body.slides.map((s: Slide) => s.narration_status).join(",");
      })
      .toBe("ready,failed,failed");

    await page.goto(`/lesson/${lesson_id}`);
    await expect(page.getByText("Slide 1 of 3")).toBeVisible();
    await expect(
      page.getByText("Narration is ready. Press Play when you are."),
    ).toBeVisible();
    // No autoplay: the slide may animate, but audio has not been started.
    expect(
      await page.evaluate(
        () => (window as unknown as { __played: number }).__played,
      ),
    ).toBe(0);
    await page.screenshot({
      path: testInfo.outputPath("narration-ready.png"),
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
    await expect
      .poll(() =>
        page.evaluate(
          () => (window as unknown as { __played: number }).__played,
        ),
      )
      .toBeGreaterThan(0);
    // Captions follow the alignment: spoken characters are marked.
    await expect(page.locator(".narration-caption mark").first()).toBeVisible();
    await expect(page.locator(".narration-caption")).toContainText(
      "The sky is a map waiting to be explored.",
    );
    await page.screenshot({
      path: testInfo.outputPath("narration-playing.png"),
      fullPage: true,
    });
    await page.getByRole("button", { name: "Pause narration" }).click();
    await expect(
      page.getByRole("button", { name: "Play narration", exact: true }),
    ).toBeVisible();

    // Failed audio (misaligned and provider failure) leaves text and test usable.
    await page.getByRole("button", { name: "Next", exact: true }).click();
    await expect(page.getByText("Slide 2 of 3")).toBeVisible();
    await expect(page.getByText("Narration unavailable")).toBeVisible();
    await expect(
      page.getByText(
        "Read the captions and transcript, then continue. The animation still plays.",
      ),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Play narration" }),
    ).toHaveCount(0);
    await page.getByRole("button", { name: "Next", exact: true }).click();
    await expect(page.getByText("Narration unavailable")).toBeVisible();
    await expect(
      page.getByRole("link", { name: "Take lesson test" }),
    ).toBeVisible();
  });
}

test("narration audio is only served to its owner and honours reduced motion", async ({
  page,
  request,
}) => {
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.goto("/profile");
  const tokens = await page.evaluate(() =>
    JSON.parse(sessionStorage.getItem("duolify.native-store-fixture")!),
  );
  const headers = { Authorization: `Bearer ${tokens.access_token}` };
  const { lesson_id } = await (
    await request.post(`${apiBase}/test/player/fixture`, { headers })
  ).json();
  await request.post(`${apiBase}/test/narration/queue`, {
    headers,
    data: { lesson_id },
  });
  await expect
    .poll(async () =>
      (
        await (
          await request.get(`${apiBase}/v1/lessons/${lesson_id}/manifest`, {
            headers,
          })
        ).json()
      ).slides.every((s: Slide) => s.narration_status === "ready"),
    )
    .toBe(true);
  const url = `${apiBase}/v1/lessons/${lesson_id}/slides/sky-1/narration/audio`;
  expect((await request.get(url)).status()).toBe(401);
  const audio = await request.get(url, { headers });
  expect(audio.status()).toBe(200);
  expect(audio.headers()["content-type"]).toContain("audio/");
  await page.goto(`/lesson/${lesson_id}`);
  await page.getByRole("button", { name: "Play narration" }).click();
  const mark = page.locator(".narration-caption mark").first();
  await expect(mark).toBeVisible();
  expect(
    await mark.evaluate((el) => getComputedStyle(el).transitionDuration),
  ).toMatch(/^0s$|^0s, 0s/);
});
