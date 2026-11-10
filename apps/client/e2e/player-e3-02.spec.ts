import { test, expect, resetTokens } from "./accounts-fixture";
import { apiBase } from "./servers";
resetTokens();
test.afterEach(() => resetTokens());

for (const width of [1280, 390]) {
  test(`control and restore playback at ${width}px`, async ({
    page,
    request,
  }, testInfo) => {
    await page.setViewportSize({ width, height: 900 });
    await page.emulateMedia({
      colorScheme: width === 390 ? "dark" : "light",
      reducedMotion: width === 390 ? "reduce" : "no-preference",
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
    await page.goto(`/lesson/${lesson_id}?returnTo=%2Fcourses`);
    await expect(page.getByText("Slide 1 of 3")).toBeVisible();
    await page.getByRole("button", { name: "Pause", exact: true }).click();
    await expect(
      page.getByRole("button", { name: "Play", exact: true }),
    ).toBeVisible();
    const time = page.getByRole("slider", { name: "Slide time" });
    await time.fill("1200");
    await page.getByRole("button", { name: "Speed 1x", exact: true }).click();
    await expect(
      page.getByRole("button", { name: "Speed 1.25x", exact: true }),
    ).toBeVisible();
    await expect(time).toHaveValue("1200");
    await page
      .getByRole("region", { name: "Lesson playback", exact: true })
      .focus();
    await page.keyboard.press("ArrowLeft");
    await expect(time).toHaveValue("200");
    await page.keyboard.press("Space");
    await expect(
      page.getByRole("button", { name: "Pause", exact: true }),
    ).toBeVisible();
    await page.keyboard.press("Space");
    await expect(
      page.getByRole("button", { name: "Play", exact: true }),
    ).toBeVisible();
    await time.fill("1200");
    if (width === 1280) {
      await expect
        .poll(() =>
          page
            .frameLocator('iframe[title="Slide 1"]')
            .locator(".star")
            .evaluate(() => {
              const animation = document.getAnimations()[0];
              return {
                time: animation.currentTime,
                state: animation.playState,
                rate: animation.playbackRate,
              };
            }),
        )
        .toEqual({ time: 1200, state: "paused", rate: 1.25 });
    }
    await page.getByRole("button", { name: "Next", exact: true }).click();
    await expect(page.locator('iframe[title="Slide 2"]')).toBeVisible();
    await page.getByRole("button", { name: "Pause", exact: true }).click();
    await time.fill("1800");
    await page.getByRole("link", { name: "Exit lesson" }).click();
    await expect(page).toHaveURL("/courses");
    const positionURL = `${apiBase}/v1/lessons/${lesson_id}/position`;
    await expect
      .poll(
        async () =>
          (await (await request.get(positionURL, { headers })).json())?.time_ms,
      )
      .toBe(1800);
    // Clearing the disposable cursor proves this restore comes from Postgres.
    await page.evaluate(() =>
      Object.keys(sessionStorage)
        .filter((k) => k.startsWith("player:position:"))
        .forEach((k) => sessionStorage.removeItem(k)),
    );
    await page.goto(`/lesson/${lesson_id}?returnTo=%2Fcourses`);
    await expect(page.getByText("Slide 2 of 3")).toBeVisible();
    await expect(time).toHaveValue("1800");
    await expect(
      page.getByRole("button", { name: "Play", exact: true }),
    ).toBeVisible();
    await page.getByRole("button", { name: "Replay slide" }).click();
    await expect
      .poll(async () => Number(await time.inputValue()))
      .toBeLessThan(1500);
    await page.getByRole("button", { name: "Transcript", exact: true }).click();
    await expect(
      page.locator(".player-controls button").filter({ hasText: /^Play$/ }),
    ).toBeDisabled();
    const paused = await page
      .locator('input[aria-label="Slide time"]')
      .inputValue();
    if (width < 768) await page.keyboard.press("Escape");
    else await page.getByRole("button", { name: "Close Transcript" }).click();
    await expect(
      page.getByRole("button", { name: "Transcript", exact: true }),
    ).toBeFocused();
    await expect(time).toHaveValue(paused);
    await page.getByRole("button", { name: "Play", exact: true }).click();
    await expect(
      page.getByText("Slide ended. Continue when you’re ready."),
    ).toBeVisible();
    await expect(time).toHaveValue("3000");
    await page.getByRole("button", { name: "Previous", exact: true }).click();
    await expect(page.getByText("Slide 1 of 3")).toBeVisible();
    await page.getByRole("button", { name: "Pause", exact: true }).click();
    await page.screenshot({
      path: testInfo.outputPath("player-controls.png"),
      fullPage: true,
    });
    await expect(page.getByText("Lesson complete!")).toHaveCount(0);
  });
}

test("active frame checkpoints, hidden pause, forged messages and unsaved recovery", async ({
  page,
  request,
}) => {
  await page.goto("/profile");
  const token = await page.evaluate(
    () =>
      JSON.parse(sessionStorage.getItem("duolify.native-store-fixture")!)
        .access_token,
  );
  const seeded = await request.post(`${apiBase}/test/player/fixture`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  const { lesson_id } = await seeded.json();
  await page.goto(`/lesson/${lesson_id}`);
  await expect(page.locator('iframe[title="Slide 1"]')).toBeVisible();
  await page.getByRole("button", { name: "Pause", exact: true }).click();
  await page.evaluate(() => window.postMessage({ type: "ended" }, "*"));
  await expect(
    page.getByText("Slide ended. Continue when you’re ready."),
  ).toHaveCount(0);
  await page.getByRole("button", { name: "Play", exact: true }).click();
  await expect(
    page.getByRole("status").filter({ hasText: "Checkpoint reached" }),
  ).toBeVisible();
  await page.evaluate(() => {
    Object.defineProperty(document, "hidden", {
      configurable: true,
      value: true,
    });
    document.dispatchEvent(new Event("visibilitychange"));
  });
  await expect(
    page.getByRole("button", { name: "Play", exact: true }),
  ).toBeVisible();
  await page.route("**/v1/lessons/*/position", (route) =>
    route.fulfill({ status: 503, json: { error: "unavailable" } }),
  );
  await page.getByRole("slider", { name: "Slide time" }).fill("900");
  await expect(
    page.getByText("Position saved on this device only."),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Retry saving position" }),
  ).toBeVisible();
  await expect(page.locator("iframe").first()).toHaveAttribute(
    "sandbox",
    "allow-scripts",
  );
});
