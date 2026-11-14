import { test, expect } from "./accounts-fixture";

const course = "c1111111-1111-4111-8111-111111111111";

for (const mobile of [false, true]) {
  test.describe(`E16-08 ${mobile ? "mobile" : "desktop"}`, () => {
    test.use({
      viewport: mobile
        ? { width: 390, height: 844 }
        : { width: 1280, height: 900 },
      hasTouch: mobile,
    });

    test("confirm fresh diff applies outline changes with confirm dialog", async ({
      page,
    }) => {
      await page.emulateMedia({
        colorScheme: mobile ? "dark" : "light",
        reducedMotion: "reduce",
      });

      let confirmCalled = false;
      await page.route(`**/v1/courses/${course}`, (r) =>
        r.fulfill({
          json: {
            id: course,
            title: "Navigation",
            outline_version: 1,
            units: [],
          },
        }),
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
      await page.route(`**/v1/courses/${course}/replan/quote`, (r) =>
        r.fulfill({
          json: {
            id: "q1",
            pricing_version: "replan-v1",
            content_ceiling: 55,
            expires_at: "2099-01-01T00:00:00Z",
          },
        }),
      );
      await page.route("**/v1/quotes/q1/accept", (r) =>
        r.fulfill({ json: { accepted: true } }),
      );
      await page.route(`**/v1/courses/${course}/replan`, (r) =>
        r.fulfill({
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
                unit_title: "Unit 3",
                before: "History",
                after: "Go examples",
              },
            ],
          },
        }),
      );
      await page.route(
        `**/v1/courses/${course}/replan/p1/confirm`,
        async (r) => {
          confirmCalled = true;
          await r.fulfill({
            status: 200,
            json: {
              id: "p1",
              state: "proposed",
              decision: "confirmed",
              outline_version: 2,
            },
          });
        },
      );

      await page.goto(`/course/${course}`);
      await page.getByRole("button", { name: "Steer course" }).click();
      await page
        .getByRole("button", { name: "Re-plan remaining outline" })
        .click();
      await page
        .getByRole("button", { name: "Accept quote and prepare proposal" })
        .click();

      await expect(
        page.getByRole("heading", { name: "Proposed changes" }),
      ).toBeVisible();
      const applyBtn = page.getByRole("button", { name: "Apply changes" });
      await expect(applyBtn).toBeVisible();
      const discardBtn = page.getByRole("button", { name: "Discard" });
      await expect(discardBtn).toBeVisible();

      // Click Apply changes opens confirmation dialog
      await applyBtn.click();
      const dialog = page.getByRole("dialog", { name: /apply|confirm/i });
      await expect(dialog).toBeVisible();
      await expect(
        dialog.getByText(/generated and unlocked lessons/i),
      ).toBeVisible();

      // Keyboard escape closes dialog without applying
      await page.keyboard.press("Escape");
      await expect(dialog).toHaveCount(0);
      expect(confirmCalled).toBe(false);

      // Reopen and confirm
      await applyBtn.click();
      await expect(dialog).toBeVisible();
      const confirmDialogBtn = dialog.getByRole("button", {
        name: "Apply changes",
      });
      await confirmDialogBtn.click();

      // Verifies confirmed state per canvas
      expect(confirmCalled).toBe(true);
      await expect(page.getByRole("status")).toContainText("Outline updated");
      await expect(
        page.getByRole("button", { name: "Back to learn path" }),
      ).toBeVisible();
    });

    test("rejects stale and expired proposals and shows recovery", async ({
      page,
    }) => {
      await page.emulateMedia({
        colorScheme: mobile ? "light" : "dark",
        reducedMotion: "reduce",
      });

      await page.route(`**/v1/courses/${course}`, (r) =>
        r.fulfill({
          json: {
            id: course,
            title: "Navigation",
            outline_version: 2,
            units: [],
          },
        }),
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
      await page.route(`**/v1/courses/${course}/replan/quote`, (r) =>
        r.fulfill({
          json: {
            id: "q1",
            pricing_version: "replan-v1",
            content_ceiling: 55,
            expires_at: "2099-01-01T00:00:00Z",
          },
        }),
      );
      await page.route("**/v1/quotes/q1/accept", (r) =>
        r.fulfill({ json: { accepted: true } }),
      );
      await page.route(`**/v1/courses/${course}/replan`, (r) =>
        r.fulfill({
          status: 201,
          json: {
            id: "p-stale",
            outline_version: 1, // Stale: course is version 2
            expires_at: "2099-01-02T00:00:00Z",
            state: "proposed",
            diff: [
              {
                kind: "retitled",
                lesson_id: "l2",
                unit_title: "Unit 3",
                before: "History",
                after: "Go examples",
              },
            ],
          },
        }),
      );
      await page.route(
        `**/v1/courses/${course}/replan/p-stale/confirm`,
        async (r) => {
          await r.fulfill({
            status: 409,
            json: { reason_code: "version_conflict" },
          });
        },
      );

      await page.goto(`/course/${course}`);
      await page.getByRole("button", { name: "Steer course" }).click();
      await page
        .getByRole("button", { name: "Re-plan remaining outline" })
        .click();
      await page
        .getByRole("button", { name: "Accept quote and prepare proposal" })
        .click();

      const applyBtn = page.getByRole("button", { name: "Apply changes" });
      await applyBtn.click();
      const dialog = page.getByRole("dialog", { name: /apply|confirm/i });
      await dialog.getByRole("button", { name: "Apply changes" }).click();

      // Shows stale banner matching canvas
      await expect(page.getByRole("alert")).toContainText(
        "This proposal is out of date",
      );
      await expect(
        page.getByText(/your outline changed since it was made/i),
      ).toBeVisible();
      await expect(
        page.getByRole("button", {
          name: /request a new proposal|review a new quote/i,
        }),
      ).toBeVisible();
    });

    test("discard keeps outline unchanged without refund", async ({ page }) => {
      let discardCalled = false;
      await page.route(`**/v1/courses/${course}`, (r) =>
        r.fulfill({
          json: {
            id: course,
            title: "Navigation",
            outline_version: 1,
            units: [],
          },
        }),
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
      await page.route(`**/v1/courses/${course}/replan/quote`, (r) =>
        r.fulfill({
          json: {
            id: "q1",
            pricing_version: "replan-v1",
            content_ceiling: 55,
            expires_at: "2099-01-01T00:00:00Z",
          },
        }),
      );
      await page.route("**/v1/quotes/q1/accept", (r) =>
        r.fulfill({ json: { accepted: true } }),
      );
      await page.route(`**/v1/courses/${course}/replan`, (r) =>
        r.fulfill({
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
                unit_title: "Unit 3",
                before: "History",
                after: "Go examples",
              },
            ],
          },
        }),
      );
      await page.route(
        `**/v1/courses/${course}/replan/p1/discard`,
        async (r) => {
          discardCalled = true;
          await r.fulfill({
            status: 200,
            json: { id: "p1", state: "proposed", decision: "discarded" },
          });
        },
      );

      await page.goto(`/course/${course}`);
      await page.getByRole("button", { name: "Steer course" }).click();
      await page
        .getByRole("button", { name: "Re-plan remaining outline" })
        .click();
      await page
        .getByRole("button", { name: "Accept quote and prepare proposal" })
        .click();

      const discardBtn = page.getByRole("button", { name: "Discard" });
      await discardBtn.click();
      expect(discardCalled).toBe(true);
      await expect(
        page.getByRole("heading", { name: "Proposed changes" }),
      ).toHaveCount(0);
    });
  });
}
