import { test, expect, resetTokens } from "./accounts-fixture";
import { apiBase } from "./servers";

resetTokens();
test.afterEach(() => resetTokens());

for (const width of [1280, 390]) {
  test(`learn with reduced motion and accessible controls at ${width}px`, async ({
    page,
    request,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    await page.emulateMedia({
      reducedMotion: "reduce",
      colorScheme: width === 390 ? "dark" : "light",
    });

    await page.goto("/profile");
    const tokens = await page.evaluate(() =>
      JSON.parse(sessionStorage.getItem("duolify.auth")!),
    );
    const headers = { Authorization: `Bearer ${tokens.access_token}` };
    const seeded = await request.post(`${apiBase}/test/player/fixture`, {
      headers,
    });
    expect(seeded.status()).toBe(201);
    const { lesson_id } = await seeded.json();

    await page.goto(`/lesson/${lesson_id}`);
    await expect(page.getByText("Slide 1 of 3")).toBeVisible();

    // Reduced motion applies data-motion="reduce" to the frame and suppresses animations
    const frame = page.locator('iframe[title="Slide 1"]');
    await expect(frame).toBeVisible();
    await expect(
      page.frameLocator('iframe[title="Slide 1"]').locator(".star"),
    ).toHaveCSS("animation-name", "none");

    // Static frame displays full instructional explanation
    const explanation = page.locator(".slide-caption");
    await expect(explanation).toBeVisible();
    await expect(explanation).toContainText(
      "The sky is a map waiting to be explored",
    );

    // Captions toggle button operates and preserves accessible state
    const captionsBtn = page.getByRole("button", {
      name: "Captions",
      exact: true,
    });
    await expect(captionsBtn).toBeVisible();
    await expect(captionsBtn).toHaveAttribute("aria-pressed", "true");
    await captionsBtn.click();
    await expect(captionsBtn).toHaveAttribute("aria-pressed", "false");
    await captionsBtn.click();
    await expect(captionsBtn).toHaveAttribute("aria-pressed", "true");

    // Narration mute keeps captions and transcript accessible
    const muteBtn = page.getByRole("button", {
      name: "Mute narration",
      exact: true,
    });
    await muteBtn.click();
    await expect(
      page.getByRole("button", { name: "Unmute narration", exact: true }),
    ).toBeVisible();
    await expect(explanation).toBeVisible();

    // 200% text resizing and 320px reflow keep controls usable without overflow
    await page.evaluate(() => {
      document.documentElement.style.fontSize = "200%";
    });
    if (width === 390) {
      await page.setViewportSize({ width: 320, height: 900 });
      await expect
        .poll(() =>
          page.evaluate(
            () => document.documentElement.scrollWidth <= window.innerWidth,
          ),
        )
        .toBe(true);
    }
    await page.evaluate(() => {
      document.documentElement.style.fontSize = "100%";
    });
    if (width === 390) {
      await page.setViewportSize({ width: 390, height: 900 });
    }

    // Keyboard controls work for play/pause and seek
    await page.getByRole("button", { name: "Pause", exact: true }).click();
    await expect(
      page.getByRole("button", { name: "Play", exact: true }),
    ).toBeVisible();
    await page
      .getByRole("region", { name: "Lesson playback", exact: true })
      .focus();
    await page.keyboard.press("Space");
    await expect(
      page.getByRole("button", { name: "Pause", exact: true }),
    ).toBeVisible();
    await page.keyboard.press("Space");
    await expect(
      page.getByRole("button", { name: "Play", exact: true }),
    ).toBeVisible();

    // Transcript panel opens, displays full explanation, and closes with focus restoration
    const transcriptBtn = page.getByRole("button", {
      name: "Transcript",
      exact: true,
    });
    await transcriptBtn.click();
    const transcriptRegion = page.getByRole(width < 768 ? "dialog" : "region", {
      name: "Transcript",
    });
    await expect(transcriptRegion).toBeVisible();
    await expect(transcriptRegion).toContainText(
      "The sky is a map waiting to be explored",
    );

    if (width < 768) {
      await page.keyboard.press("Escape");
    } else {
      await page
        .getByRole("button", { name: "Close Transcript", exact: true })
        .click();
    }
    await expect(transcriptBtn).toBeFocused();
  });

  test(`missing asset shows recovery with retry and exit without completion at ${width}px`, async ({
    page,
    request,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    await page.goto("/profile");
    const tokens = await page.evaluate(() =>
      JSON.parse(sessionStorage.getItem("duolify.auth")!),
    );
    const headers = { Authorization: `Bearer ${tokens.access_token}` };
    const seeded = await request.post(`${apiBase}/test/player/fixture`, {
      headers,
    });
    expect(seeded.status()).toBe(201);
    const { lesson_id } = await seeded.json();

    // Intercept slide 2 to simulate missing asset / failure
    await page.route(`**/v1/lessons/${lesson_id}/slides/sky-2`, (route) =>
      route.fulfill({
        status: 503,
        json: { error: "asset_unavailable" },
      }),
    );

    await page.goto(`/lesson/${lesson_id}?returnTo=%2Fcourses`);
    await expect(page.getByText("Slide 1 of 3")).toBeVisible();

    // Navigate to slide 2 which has the missing asset
    await page.getByRole("button", { name: "Next", exact: true }).click();
    await expect(page.getByText("Slide 2 of 3")).toBeVisible();

    // Canvas stage failure card appears
    await expect(page.getByText("This slide didn't load")).toBeVisible();
    await expect(
      page.getByText("A diagram asset is missing. Your place is saved."),
    ).toBeVisible();

    // Recovery action controls are present
    const retryBtn = page.getByRole("button", {
      name: "Retry slide",
      exact: true,
    });
    await expect(retryBtn).toBeVisible();

    const exitBtn = page.getByRole("button", {
      name: "Exit to path",
      exact: true,
    });
    await expect(exitBtn).toBeVisible();

    // Next button is disabled and completion is never granted
    const nextBtn = page.getByRole("button", { name: /Next/ });
    await expect(nextBtn).toBeDisabled();

    // Transcript for the slide remains accessible below the frame / in panel
    const transcriptBtn = page.getByRole("button", {
      name: "Transcript",
      exact: true,
    });
    await expect(transcriptBtn).toBeVisible();
    await transcriptBtn.click();
    await expect(
      page.getByRole(width < 768 ? "dialog" : "region", {
        name: "Transcript",
      }),
    ).toBeVisible();
    if (width < 768) {
      await page.keyboard.press("Escape");
    } else {
      await page
        .getByRole("button", { name: "Close Transcript", exact: true })
        .click();
    }

    // Previous button remains functional
    const prevBtn = page.getByRole("button", {
      name: "Previous",
      exact: true,
    });
    await expect(prevBtn).toBeEnabled();

    // Exiting to path preserves position and redirects to returnTo path
    await exitBtn.click();
    await expect(page).toHaveURL("/courses");

    // Reopen lesson to verify position was preserved on slide 2
    await page.goto(`/lesson/${lesson_id}?returnTo=%2Fcourses`);
    await expect(page.getByText("Slide 2 of 3")).toBeVisible();
    await expect(page.getByText("This slide didn't load")).toBeVisible();
  });
}
