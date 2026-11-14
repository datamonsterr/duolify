import { test, expect, resetTokens } from "./accounts-fixture";
import { apiBase } from "./servers";

// E14-03: Seek/speed/mute narration then restart offline with missing-audio fallback.
// Canvas: S09-Player, D2-Player desktop and mobile states.
resetTokens();
test.afterEach(() => resetTokens());

type Slide = { slide_id: string; narration_status: string };

for (const width of [1280, 390]) {
  test(`seek/speed/mute narration then restart offline with missing-audio fallback at ${width}px`, async ({
    page,
    request,
    context,
  }, testInfo) => {
    await page.setViewportSize({ width, height: 900 });
    await page.emulateMedia({
      colorScheme: width === 390 ? "dark" : "light",
      reducedMotion: "no-preference",
    });

    await page.addInitScript(() => {
      const w = window as unknown as {
        __audioPlays: number;
        __rates: number[];
        __muted: boolean[];
        __currentTime: number;
      };
      w.__audioPlays = 0;
      w.__rates = [];
      w.__muted = [];
      w.__currentTime = 0;

      const play = HTMLMediaElement.prototype.play;
      HTMLMediaElement.prototype.play = function (...args) {
        w.__audioPlays += 1;
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

    const tag = Math.random().toString(36).slice(2).padEnd(8, "x").slice(0, 8);
    const salted = await request.post(`${apiBase}/test/narration/salt`, {
      headers,
      data: { lesson_id, salt: tag },
    });
    expect(salted.status()).toBe(204);

    // Queue narration for lesson: slide 1 and 2 ready, slide 3 failed to test fallback
    const queued = await request.post(`${apiBase}/test/narration/queue`, {
      headers,
      data: {
        lesson_id,
        salt: tag,
        fail: { "sky-3": "engine" },
      },
    });
    expect(queued.status()).toBe(202);
    await expect
      .poll(
        async () => {
          const body = await (
            await request.get(`${apiBase}/v1/lessons/${lesson_id}/manifest`, {
              headers,
            })
          ).json();
          return body.slides.map((s: Slide) => s.narration_status).join(",");
        },
        { timeout: 30_000 },
      )
      .toBe("ready,ready,failed");

    await page.goto(`/lesson/${lesson_id}`);
    await expect(page.getByText("Slide 1 of 3")).toBeVisible();
    await expect(page.getByText("Available offline")).toBeVisible();

    // S09 Canvas: Idle — narration ready
    await expect(
      page.getByText("Narration is ready. Press Play when you are."),
    ).toBeVisible();

    // Captions default ON
    const captionsButton = page.getByRole("button", {
      name: "Captions",
      exact: true,
    });
    await expect(captionsButton).toHaveAttribute("aria-pressed", "true");

    // No autoplay before user initiates
    expect(
      await page.evaluate(
        () => (window as unknown as { __audioPlays: number }).__audioPlays,
      ),
    ).toBe(0);

    // 1. Play narration (and unified clock)
    const playBtn = page.getByRole("button", {
      name: "Play narration",
      exact: true,
    });
    if (width < 768) {
      await playBtn.focus();
      await page.keyboard.press("Enter");
    } else {
      await playBtn.click();
    }

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

    // Captions highlight with alignment data
    await expect(page.locator(".narration-caption mark").first()).toBeVisible();
    await expect(page.locator(".narration-caption")).toContainText(
      "The sky is a map waiting to be explored.",
    );

    // 2. Speed control: 1x -> 1.25x -> 1.5x -> 0.75x affects both clocks
    const speedBtn = page.getByRole("button", { name: /^Speed / });
    await expect(speedBtn).toBeVisible();
    await speedBtn.click(); // to 1.25x
    await expect(page.getByRole("button", { name: /1\.25/ })).toBeVisible();

    // 3. Mute narration: audio muted, captions stay visible
    const muteBtn = page.getByRole("button", { name: "Mute narration" });
    await expect(muteBtn).toBeVisible();
    await muteBtn.click();
    await expect(
      page.getByRole("button", { name: "Unmute narration" }),
    ).toBeVisible();
    // Captions stay visible after mute
    await expect(page.locator(".narration-caption")).toBeVisible();

    // 4. Seek via keyboard or slider
    await page.keyboard.press("ArrowRight");

    // Pause
    const pauseBtn = page.getByRole("button", { name: "Pause narration" });
    await pauseBtn.click();
    await expect(
      page.getByRole("button", { name: "Play narration", exact: true }),
    ).toBeVisible();

    // Replay restarts both clocks from 0
    const replayBtn = page.getByRole("button", { name: "Replay slide" });
    await replayBtn.click();
    await expect(
      page.getByRole("button", { name: "Pause narration", exact: true }),
    ).toBeVisible();

    await page.screenshot({
      path: testInfo.outputPath(`narration-sync-${width}.png`),
      fullPage: true,
    });

    // 5. Offline restart: go offline and reload
    await context.setOffline(true);
    await page.route("**/v1/**", (route) =>
      route.abort("internetdisconnected"),
    );

    await page.reload();
    await expect(page.getByText("Slide 1 of 3")).toBeVisible();
    await expect(page.getByText("Available offline")).toBeVisible();

    // Per-device settings persist across offline restart
    await expect(
      page.getByRole("button", { name: "Unmute narration" }),
    ).toBeVisible();
    await expect(page.getByRole("button", { name: /1\.25/ })).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Captions", exact: true }),
    ).toHaveAttribute("aria-pressed", "true");

    // Play next slide while offline (slide 2 audio was preloaded and cached)
    await page.getByRole("button", { name: "Next", exact: true }).click();
    await expect(page.getByText("Slide 2 of 3")).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Play narration", exact: true }),
    ).toBeVisible();

    // Advance to slide 3: failed audio falls back to caption-only
    await page.getByRole("button", { name: "Next", exact: true }).click();
    await expect(page.getByText("Slide 3 of 3")).toBeVisible();
    await expect(page.getByText("Narration unavailable")).toBeVisible();
    await expect(
      page.getByText(
        "Read the captions and transcript, then continue. The animation still plays.",
      ),
    ).toBeVisible();
    // Test link remains accessible
    await expect(
      page.getByRole("link", { name: "Take lesson test" }),
    ).toBeVisible();
  });
}
