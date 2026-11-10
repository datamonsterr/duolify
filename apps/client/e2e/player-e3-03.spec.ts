import { test, expect, resetTokens } from "./accounts-fixture";
import { apiBase } from "./servers";
resetTokens();
test.afterEach(() => resetTokens());
for (const width of [1280, 390]) {
  test(`opened lesson restarts offline with every slide at ${width}px`, async ({
    page,
    request,
    context,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    await page.emulateMedia({
      colorScheme: width === 390 ? "dark" : "light",
      reducedMotion: "reduce",
    });
    await page.goto("/profile");
    const token = await page.evaluate(
      () => JSON.parse(sessionStorage.getItem("duolify.auth")!).access_token,
    );
    const seed = await request.post(`${apiBase}/test/player/fixture`, {
      headers: { Authorization: `Bearer ${token}` },
    });
    const { lesson_id } = await seed.json();
    await page.goto(`/lesson/${lesson_id}`);
    await expect(page.getByText("Available offline")).toBeVisible();
    await page.evaluate(async () => {
      await navigator.serviceWorker.ready;
    });
    await context.setOffline(true);
    // Fixture routes use APIRequestContext, so abort them explicitly too.
    await page.route("**/v1/**", (route) =>
      route.abort("internetdisconnected"),
    );
    await page.reload();
    await expect(page.getByText("Available offline")).toBeVisible();
    for (let n = 1; n <= 3; n++) {
      await expect(page.locator(`iframe[title="Slide ${n}"]`)).toBeVisible();
      await expect(page.locator(`iframe[title="Slide ${n}"]`)).toHaveAttribute(
        "sandbox",
        "allow-scripts",
      );
      await expect(
        page.frameLocator(`iframe[title="Slide ${n}"]`).locator(".star"),
      ).toBeVisible();
      if (n < 3)
        await page.getByRole("button", { name: "Next", exact: true }).click();
    }
    await page.goto("/lesson/uncached");
    await expect(page.getByText("This lesson isn't downloaded")).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Retry", exact: true }),
    ).toBeVisible();
    await page.getByRole("link", { name: "Exit lesson" }).focus();
    await expect(page.getByRole("link", { name: "Exit lesson" })).toBeFocused();
  });
}
