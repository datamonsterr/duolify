import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "./accounts-fixture";
import { apiBase } from "./servers";

// Quick notes are audited separately. Dynamic detail routes need populated fixtures.
for (const path of [
  "/",
  "/courses",
  "/courses/create",
  "/marketplace",
  "/moderation",
  "/moderation/notices",
  "/profile",
  "/profile/setup",
  "/credits",
  "/credits/billing",
  "/more",
  "/friends",
  "/leagues",
  "/bookmarks",
  "/settings",
  "/settings/sessions",
  "/settings/characters",
  "/streak",
  "/analytics",
]) {
  test(`dark contrast: ${path}`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
    await page.goto(path);
    await expect(page.locator("main")).toBeVisible();
    if (path === "/settings/sessions")
      await expect(page.locator(".sessions-panel")).toHaveAttribute(
        "aria-busy",
        "false",
      );
    const results = await new AxeBuilder({ page })
      .withRules(["color-contrast"])
      .analyze();
    expect(results.violations).toEqual([]);
  });
}

test("dark contrast: populated lesson and transcript", async ({
  page,
  request,
}) => {
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
  await page.goto("/profile");
  const token = await page.evaluate(
    () =>
      JSON.parse(sessionStorage.getItem("duolify.native-store-fixture")!)
        .access_token as string,
  );
  const seeded = await request.post(`${apiBase}/test/player/fixture`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  expect(seeded.status()).toBe(201);
  const { lesson_id } = await seeded.json();
  await page.goto(`/lesson/${lesson_id}`);
  await expect(page.getByText("Slide 1 of 3")).toBeVisible();
  await page.getByRole("button", { name: "Transcript", exact: true }).click();
  await expect(page.getByRole("region", { name: "Transcript" })).toBeVisible();
  expect(
    (await new AxeBuilder({ page }).withRules(["color-contrast"]).analyze())
      .violations,
  ).toEqual([]);
});

test("dark contrast: populated course publication preview", async ({
  page,
  request,
}) => {
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
  await page.goto("/profile");
  const token = await page.evaluate(
    () =>
      JSON.parse(sessionStorage.getItem("duolify.native-store-fixture")!)
        .access_token as string,
  );
  const seeded = await request.post(`${apiBase}/test/sharing/course`, {
    headers: { Authorization: `Bearer ${token}` },
    data: { variant: "safe" },
  });
  expect(seeded.status()).toBe(201);
  const { course_id } = await seeded.json();
  await page.route(
    /\/v1\/(courses|features|gamification)(\/|$|\?)/,
    async (route) => {
      const original = route.request();
      const target = new URL(original.url());
      target.host = new URL(apiBase).host;
      target.protocol = new URL(apiBase).protocol;
      const headers = original.headers();
      delete headers.origin;
      await route.fulfill({
        response: await request.fetch(target.href, {
          method: original.method(),
          headers,
          data: original.postData() ?? undefined,
        }),
      });
    },
  );
  await page.goto(`/course/${course_id}`);
  await page.getByRole("button", { name: "Preview publication draft" }).click();
  await expect(page.getByTestId("draft-verdict")).toHaveText(
    "Passed screening",
  );
  expect(
    (await new AxeBuilder({ page }).withRules(["color-contrast"]).analyze())
      .violations,
  ).toEqual([]);
});
