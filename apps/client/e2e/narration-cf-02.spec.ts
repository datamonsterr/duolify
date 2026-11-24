import { test, expect, resetTokens } from "./accounts-fixture";
import { apiBase } from "./servers";

resetTokens();
test.afterEach(() => resetTokens());

test("estimated narration follows seek and speed and replays offline", async ({
  page,
  request,
  context,
}) => {
  await page.goto("/profile");
  const tokens = await page.evaluate(() =>
    JSON.parse(sessionStorage.getItem("duolify.native-store-fixture")!),
  );
  const headers = { Authorization: `Bearer ${tokens.access_token}` };
  const fixture = await request.post(`${apiBase}/test/player/fixture`, {
    headers,
  });
  expect(fixture.status()).toBe(201);
  const { lesson_id } = await fixture.json();
  const salt = Math.random().toString(36).slice(2, 10).padEnd(8, "x");
  const salted = await request.post(`${apiBase}/test/narration/salt`, {
    headers,
    data: { lesson_id, salt },
  });
  expect(salted.status()).toBe(204);
  const queued = await request.post(`${apiBase}/test/narration/queue`, {
    headers,
    data: {
      lesson_id,
      salt,
      openrouter: true,
      fail: { "sky-2": "openrouter_outage", "sky-3": "engine" },
    },
  });
  expect(queued.status(), await queued.text()).toBe(202);
  await expect
    .poll(
      async () => {
        const manifest = await request.get(
          `${apiBase}/v1/lessons/${lesson_id}/manifest`,
          { headers },
        );
        const { slides } = await manifest.json();
        return slides.map(
          (s: { narration_status: string }) => s.narration_status,
        );
      },
      { timeout: 30_000 },
    )
    .toEqual(["ready", "ready", "failed"]);

  for (const [id, source] of [
    ["sky-1", "estimated"],
    ["sky-2", "vendor"],
  ]) {
    const alignment = await request.get(
      `${apiBase}/v1/lessons/${lesson_id}/slides/${id}/narration/alignment`,
      { headers },
    );
    expect(alignment.status()).toBe(200);
    const body = await alignment.json();
    expect(body.timing_source).toBe(source);
    expect(body.end_ms.at(-1)).toBe(body.duration_ms);
  }

  await page.goto(`/lesson/${lesson_id}`);
  await expect(page.getByText("Available offline")).toBeVisible();
  await page.getByRole("button", { name: "Speed 1x", exact: true }).click();
  await expect(page.getByRole("button", { name: "Speed 1.25x" })).toBeVisible();
  await page
    .getByRole("button", { name: "Play narration", exact: true })
    .click();
  const timeline = page.getByRole("slider", { name: "Slide time" });
  const highlight = page.locator(".narration-caption mark").first();
  await expect(highlight).toBeVisible();
  await timeline.press("End");
  await expect(highlight).toContainText(
    "The sky is a map waiting to be explored.",
  );

  await context.setOffline(true);
  await page.route("**/v1/**", (route) => route.abort("internetdisconnected"));
  await page.reload();
  await expect(page.getByText("Available offline")).toBeVisible();
  await page
    .getByRole("button", { name: "Play narration", exact: true })
    .click();
  await expect(page.locator(".narration-caption mark").first()).toBeVisible();
  await page.getByRole("button", { name: "Next", exact: true }).click();
  await expect(page.getByText("Slide 2 of 3")).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Play narration" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Next", exact: true }).click();
  await expect(page.getByText("Narration unavailable")).toBeVisible();
  await expect(
    page.getByRole("link", { name: "Take lesson test" }),
  ).toBeVisible();
});
