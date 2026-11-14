import { test, expect } from "./accounts-fixture";

const course = "c1111111-1111-4111-8111-111111111111";
for (const mobile of [false, true]) {
  test.describe(`E16-07 ${mobile ? "mobile" : "desktop"}`, () => {
    test.use({
      viewport: mobile
        ? { width: 390, height: 844 }
        : { width: 1280, height: 900 },
      hasTouch: mobile,
    });
    test("quote consent precedes work and immutable diff survives closing", async ({
      page,
    }) => {
      await page.emulateMedia({
        colorScheme: mobile ? "dark" : "light",
        reducedMotion: "reduce",
      });
      let accepted = false;
      let work = 0;
      let quoteFailures = 1;
      await page.route(`**/v1/courses/${course}`, (r) =>
        r.fulfill({ json: { id: course, title: "Navigation", units: [] } }),
      );
      await page.route(`**/v1/courses/${course}/instructions`, (r) =>
        r.fulfill({
          json: {
            instructions: [],
            limits: {
              max_characters: 500,
              max_active: 20,
              max_total_characters: 4000,
              adds_per_hour: 10,
            },
            active_count: 0,
            active_characters: 0,
          },
        }),
      );
      await page.route(`**/v1/courses/${course}/replan/quote`, (r) => {
        if (quoteFailures-- > 0) return r.abort("failed");
        return r.fulfill({
          json: {
            id: "q1",
            pricing_version: "replan-v1",
            content_ceiling: 55,
            expires_at: "2099-01-01T00:00:00Z",
          },
        });
      });
      await page.route("**/v1/quotes/q1/accept", (r) => {
        accepted = true;
        return r.fulfill({ json: { accepted: true } });
      });
      await page.route(`**/v1/courses/${course}/replan`, async (r) => {
        expect(accepted).toBe(true);
        expect(r.request().postDataJSON()).toEqual({ quote_id: "q1" });
        work++;
        await r.fulfill({
          status: 201,
          json: {
            id: "p1",
            outline_version: 1,
            expires_at: "2099-01-02T00:00:00Z",
            state: "proposed",
            diff: [
              {
                kind: "retitled",
                lesson_id: "l2",
                unit_title: "Next unit",
                before: "History",
                after: "Go examples",
              },
              {
                kind: "added",
                lesson_id: "l3",
                unit_title: "Next unit",
                after: "語".repeat(500),
              },
            ],
          },
        });
      });
      await page.goto(`/course/${course}`);
      await page.getByRole("button", { name: "Steer course" }).click();
      await page
        .getByRole("button", { name: "Re-plan remaining outline" })
        .click();
      await expect(page.getByRole("alert")).toContainText(
        "Couldn’t load a quote",
      );
      await page.getByRole("button", { name: "Retry quote" }).click();
      await expect(
        page.getByText("Up to 55 credits", { exact: true }),
      ).toBeVisible();
      await expect(page.getByText(/charged even if/i)).toBeVisible();
      expect(work).toBe(0);
      const consent = page.getByRole("button", {
        name: "Accept quote and prepare proposal",
      });
      await consent.focus();
      await page.keyboard.press("Enter");
      await expect(
        page.getByRole("heading", { name: "Proposed changes" }),
      ).toBeVisible();
      await expect(
        page.getByText("Generated and unlocked lessons stay unchanged."),
      ).toBeVisible();
      await expect(page.getByText("Retitled", { exact: true })).toBeVisible();
      await expect(
        page.getByText("Go examples", { exact: true }),
      ).toBeVisible();
      await expect(page.getByText("Added", { exact: true })).toBeVisible();
      expect(
        await page
          .getByLabel("Re-plan outline")
          .evaluate((n) => n.scrollWidth > n.clientWidth + 1),
      ).toBe(false);
      await expect(
        page.getByText("Proposal ready. Your course has not changed."),
      ).toBeVisible();
      await page.getByRole("button", { name: "Back to instructions" }).click();
      await page
        .getByRole("button", { name: "Re-plan remaining outline" })
        .click();
      await expect(
        page.getByRole("heading", { name: "Proposed changes" }),
      ).toBeVisible();
      await expect(
        page.getByText("Proposal ready. Your course has not changed."),
      ).toBeVisible();
      expect(work).toBe(1);
      await page.keyboard.press("Escape");
      await expect(
        page.getByRole("button", { name: "Steer course" }),
      ).toBeFocused();
    });
  });
}
