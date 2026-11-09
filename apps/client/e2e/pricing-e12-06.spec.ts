import { expect, test } from "./accounts-fixture";
import { apiBase } from "./servers";

test.use({ hasTouch: true });

for (const width of [1280, 390]) {
  test(`scheduled price flip preserves accepted quote terms at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    let activeVersion = "schedule-before";
    await page.route("**/v1/pricing", async (route) => {
      const response = await page.request.get(apiBase + "/v1/pricing");
      const catalogue = await response.json();
      catalogue.version = activeVersion;
      catalogue.actions.preview.credits =
        activeVersion === "schedule-before" ||
        activeVersion === "rollback-higher"
          ? 25
          : 20;
      await route.fulfill({ json: catalogue });
    });
    await page.route("**/v1/quotes**", async (route) => {
      if (route.request().method() === "POST") {
        await route.fulfill({
          status: 201,
          json: {
            id: "quote-before-flip",
            pricing_version: "schedule-before",
            action: "lesson_content",
            currency: "USD",
            content_ceiling: 140,
            audio_ceiling: 50,
            daily_free_eligible: false,
            expires_at: new Date(Date.now() + 600_000).toISOString(),
            accepted: false,
          },
        });
        return;
      }
      await route.fulfill({ status: 200, json: { accepted: true } });
    });

    await page.goto("/credits");
    await page.getByRole("button", { name: "Course creation costs" }).click();
    await page.getByRole("button", { name: "Get action quote" }).click();
    await expect(
      page.getByText(/Original terms · schedule-before/),
    ).toBeVisible();
    await page.getByRole("button", { name: "Accept quote terms" }).focus();
    await page.keyboard.press("Enter");
    await expect(
      page.getByRole("status").filter({ hasText: "Terms accepted" }),
    ).toBeVisible();

    activeVersion = "schedule-after";
    await page.getByRole("button", { name: "Refresh prices" }).click();
    await expect(page.getByText("20 credits", { exact: true })).toBeVisible();
    activeVersion = "rollback-higher";
    await page.getByRole("button", { name: "Refresh prices" }).click();
    await expect(page.getByText("25 credits", { exact: true })).toBeVisible();
    await expect(
      page.getByText(/Original terms · schedule-before/),
    ).toBeVisible();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
  });
}
