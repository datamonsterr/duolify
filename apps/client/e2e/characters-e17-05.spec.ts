import { test, expect } from "./accounts-fixture";
import { apiBase } from "./servers";
import { readFileSync } from "node:fs";
import {
  captureCharacterReview,
  characterReviewViews,
} from "./characters-review";

const documents = JSON.parse(
  readFileSync(
    new URL(
      "../../../services/api/internal/characters/schema/v2.fixtures.json",
      import.meta.url,
    ),
    "utf8",
  ),
).cases;

for (const view of characterReviewViews) {
  test(`pinned generated cast follows playback at ${view.width}px ${view.color}`, async ({
    page,
    request,
    context,
  }) => {
    await page.setViewportSize({ width: view.width, height: view.height });
    await page.emulateMedia({
      reducedMotion: "reduce",
      colorScheme: view.color,
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
      manifest.cast.speakers.guide.document = documents.find(
        (c: { name: string }) => c.name === "valid robot",
      ).spec;
      manifest.cast.speakers.supporter.document = documents.find(
        (c: { name: string }) => c.name === "valid animal",
      ).spec;
      for (const slide of manifest.slides)
        slide.segments = [
          {
            speaker_key: "guide",
            speaker_label: "Pinned guide",
            text: "Observe the sky.",
            expression: "happy",
            pose: "point",
            start_ms: 0,
            duration_ms: 1500,
          },
          {
            speaker_key: "supporter",
            speaker_label: "Pinned supporter",
            text: "Earth turns.",
            expression: "thinking",
            pose: "wave",
            start_ms: 1500,
            duration_ms: 1500,
          },
        ];
      manifest.slides[1].segments[1].speaker_key = "supporter_2";
      manifest.cast.speakers.supporter_2 = {
        ...manifest.cast.speakers.supporter,
        speaker_key: "supporter_2",
        suppressed: true,
      };
      manifest.slides[2].segments[1].speaker_key = "missing";
      await route.fulfill({ json: manifest });
    });
    await page.goto(`/lesson/${lesson_id}`);
    await expect(page.getByText("Slide 1 of 3")).toBeVisible();
    await page.getByRole("button", { name: "Pause", exact: true }).click();
    const frame = page.frameLocator('iframe[title="Slide 1"]');
    const time = page.getByRole("slider", { name: "Slide time" });
    await time.fill("0");
    await expect(
      frame.locator('[data-speaker-key="guide"] svg'),
    ).toHaveAttribute("data-pose", "point");
    await time.fill("1800");
    await expect(
      frame.locator('[data-speaker-key="supporter"]'),
    ).toHaveAttribute("data-expression", "thinking");
    await expect(
      frame.locator('[data-speaker-key="supporter"] svg'),
    ).toHaveAttribute("data-pose", "wave");
    await expect(frame.locator("#dialogue-text")).toHaveText("Earth turns.");
    await page.evaluate(() => window.scrollTo(0, 0));
    await captureCharacterReview(page, "lesson", view);
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
    await expect(page.getByText("Available offline")).toBeVisible();
    await page.evaluate(async () => {
      await navigator.serviceWorker.ready;
    });
    await context.setOffline(true);
    await page.route("**/v1/**", (route) =>
      route.abort("internetdisconnected"),
    );
    await page.reload();
    await expect(page.getByText("Available offline")).toBeVisible();
    await expect(
      frame.locator('[data-speaker-key="guide"] svg'),
    ).toHaveAttribute("data-pose", "point");
    for (const [index, key, reason] of [
      [2, "supporter_2", "suppressed"],
      [3, "missing", "missing_speaker"],
    ] as const) {
      await page.getByRole("button", { name: "Next", exact: true }).click();
      const fallback = page.frameLocator(`iframe[title="Slide ${index}"]`);
      await expect(
        fallback.locator(`[data-speaker-key="${key}"] svg`),
      ).toBeVisible();
      await expect(fallback.locator(".cast-stage")).toHaveAttribute(
        "data-cast-fallback",
        new RegExp(reason),
      );
    }
  });
}
