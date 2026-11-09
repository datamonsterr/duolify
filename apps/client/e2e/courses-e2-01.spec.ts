import { test, expect } from "./accounts-fixture";
import { apiBase } from "./servers";

for (const mobile of [false, true]) {
  test(`confirmed paid preview ${mobile ? "mobile" : "desktop"}`, async ({
    page,
    request,
  }) => {
    await page.setViewportSize(
      mobile ? { width: 390, height: 844 } : { width: 1280, height: 900 },
    );
    await page.emulateMedia({
      colorScheme: mobile ? "dark" : "light",
      reducedMotion: "reduce",
    });
    let calls = 0;
    await page.route("**/v1/me/credits", (route) =>
      route.fulfill({ json: { purchased: 400, subscription: 0 } }),
    );
    let revision = "";
    await page.route("**/v1/course-brief/**", async (route) => {
      const original = route.request();
      const target = new URL(original.url());
      target.host = new URL(apiBase).host;
      const response = await request.fetch(target.href, {
        method: original.method(),
        headers: Object.fromEntries(
          Object.entries(original.headers()).filter(
            ([key]) => key !== "origin",
          ),
        ),
        data: original.postData() ?? undefined,
      });
      if (target.pathname.endsWith("/confirm"))
        revision = (await response.json()).id;
      await route.fulfill({ response });
    });
    await page.route("**/v1/quotes", (route) =>
      route.fulfill({
        json: {
          id: "preview-quote",
          action: "preview",
          pricing_version: "fixture-v1",
          currency: "USD",
          content_ceiling: 25,
          audio_ceiling: 0,
          daily_free_eligible: false,
          expires_at: new Date(Date.now() + 300000).toISOString(),
          accepted: false,
        },
      }),
    );
    await page.route("**/v1/quotes/preview-quote/accept", (route) =>
      route.fulfill({ json: { accepted: true } }),
    );
    await page.route("**/v1/courses/preview", async (route) => {
      calls++;
      expect(route.request().headers()["x-quote-id"]).toBe("preview-quote");
      expect(route.request().postDataJSON()).toEqual({
        brief_revision: revision,
      });
      await route.fulfill({
        json: {
          sample_outline: {
            title: "Sky",
            units: [
              {
                title: "Moon",
                lessons: [{ title: "Phases", goal: "Explain phases" }],
              },
            ],
          },
          example_slides: [
            {
              slide_id: "sample-1",
              html: '<div id="root">Moon phases</div>',
              narration: "Moon phases explained",
              duration_ms: 3000,
            },
          ],
        },
      });
    });
    await page.goto("/courses/create");
    await expect(
      page.getByText("Clarifying: 0 credits", { exact: false }),
    ).toBeVisible();
    await page.getByRole("link", { name: "Prefer a form?" }).click();
    await page.getByLabel("Topic", { exact: true }).fill("Astronomy");
    await page.getByLabel("Maths", { exact: true }).fill("No maths");
    expect(calls).toBe(0);
    await page
      .getByRole("button", { name: "Confirm brief", exact: true })
      .click();
    await page
      .getByRole("button", { name: "Review preview cost", exact: true })
      .click();
    await expect(page.getByText("fixture-v1", { exact: false })).toBeVisible();
    expect(calls).toBe(0);
    await page
      .getByRole("button", { name: "Confirm & preview", exact: false })
      .click();
    await expect(
      page.getByRole("heading", { name: "Course preview", exact: true }),
    ).toBeVisible();
    await expect(
      page.locator('iframe[title="Preview sample 1"]'),
    ).toHaveAttribute("sandbox", "allow-scripts");
    await expect(page.getByText("Moon phases explained")).toBeVisible();
    await page.getByRole("button", { name: "Review sample outline" }).click();
    await expect(
      page.getByRole("heading", { name: "Sample outline" }),
    ).toBeVisible();
    await page.getByRole("button", { name: "Back to preview" }).click();
    await page.getByRole("button", { name: "Discard preview" }).click();
    await expect(page.getByLabel("Maths", { exact: true })).toHaveValue(
      "No maths",
    );
    expect(calls).toBe(1);
  });
}
