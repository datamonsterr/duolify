import { test, expect } from "./accounts-fixture";
import { apiBase } from "./servers";

test.use({ hasTouch: true });

for (const width of [1280, 390]) {
  test(`refreshes a changed catalogue and labels offline prices at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    let version = "flip-a";
    let offline = false;
    await page.route("**/v1/pricing", async (route) => {
      if (offline) return route.abort("internetdisconnected");
      const response = await page.request.get(apiBase + "/v1/pricing");
      const catalogue = await response.json();
      catalogue.version = version;
      catalogue.actions.preview.credits = version === "flip-a" ? 25 : 31;
      await route.fulfill({ json: catalogue });
    });

    await page.goto("/credits");
    await page.getByRole("button", { name: "Course creation costs" }).click();
    await expect(page.getByText("25 credits", { exact: true })).toBeVisible();
    version = "flip-b";
    await page.getByRole("button", { name: "Refresh prices" }).click();
    await expect(page.getByText("31 credits", { exact: true })).toBeVisible();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);

    offline = true;
    await page.getByRole("button", { name: "Refresh prices" }).click();
    await expect(
      page.getByText(/Offline · Last known prices · flip-b/),
    ).toBeVisible();
    await expect(page.getByText("31 credits", { exact: true })).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Get action quote" }),
    ).toBeEnabled();
  });
}
