import { test, expect } from "./accounts-fixture";
import { apiBase } from "./servers";

for (const width of [1280, 390]) {
  test(`pinned generated cast follows playback at ${width}px`, async ({
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
      JSON.parse(sessionStorage.getItem("duolify.native-store-fixture")!),
    );
    const headers = { Authorization: `Bearer ${tokens.access_token}` };
    const seeded = await request.post(`${apiBase}/test/player/fixture`, {
      headers,
    });
    expect(seeded.status()).toBe(201);
    const { lesson_id } = await seeded.json();
    const defaults = await request.get(`${apiBase}/v1/characters/default-cast`);
    expect(defaults.ok()).toBeTruthy();
    const fixture = await defaults.json();
    await page.route(`**/v1/lessons/${lesson_id}/manifest`, async (route) => {
      const response = await request.get(
        `${apiBase}/v1/lessons/${lesson_id}/manifest`,
        { headers },
      );
      const manifest = await response.json();
      manifest.cast = fixture.cast;
      for (const slide of manifest.slides)
        slide.segments = [
          {
            speaker_key: "guide",
            speaker_label: "Pinned guide",
            text: "Observe the sky.",
            expression: "happy",
            start_ms: 0,
            duration_ms: 1500,
          },
          {
            speaker_key: "supporter",
            speaker_label: "Pinned supporter",
            text: "Earth turns.",
            expression: "thinking",
            start_ms: 1500,
            duration_ms: 1500,
          },
        ];
      await route.fulfill({ json: manifest });
    });
    await page.goto(`/lesson/${lesson_id}`);
    await expect(page.getByText("Slide 1 of 3")).toBeVisible();
    await page.getByRole("button", { name: "Pause", exact: true }).click();
    const frame = page.frameLocator('iframe[title="Slide 1"]');
    const time = page.getByRole("slider", { name: "Slide time" });
    await time.fill("1800");
    await expect(
      frame.locator('[data-speaker-key="supporter"]'),
    ).toHaveAttribute("data-expression", "thinking");
    await expect(frame.locator("#dialogue-text")).toHaveText("Earth turns.");
    await page.getByRole("button", { name: "Speed 1x", exact: true }).click();
    await expect(time).toHaveValue("1800");
    await time.fill("0");
    await expect(frame.locator("#dialogue-text")).toHaveText(
      "Observe the sky.",
    );
    await expect(page.locator('iframe[title="Slide 1"]')).toHaveAttribute(
      "sandbox",
      "allow-scripts",
    );
    await page.getByRole("button", { name: "Replay slide" }).click();
    await page.getByRole("button", { name: "Pause", exact: true }).click();
    await time.fill("1800");
    await expect(frame.locator("#dialogue-text")).toHaveText("Earth turns.");
  });
}
