import { test, expect, resetTokens } from "./accounts-fixture";

test.describe("E1-07 account export and deletion", () => {
  test.afterEach(() => {
    resetTokens();
  });
  test("supports mobile touch and cancel recovery", async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto("/settings");
    await expect(page.getByRole("heading", { name: "Settings" })).toBeVisible();

    // Cancel deletion dialog
    await page.getByRole("button", { name: "Delete account" }).click();
    await expect(
      page.getByRole("dialog", { name: "Delete your account?" }),
    ).toBeVisible();
    await page.getByRole("button", { name: "Cancel" }).click();
    await expect(
      page.getByRole("dialog", { name: "Delete your account?" }),
    ).not.toBeVisible();

    // Cancel export dialog
    await page.getByRole("button", { name: "Export data" }).click();
    await expect(
      page.getByRole("dialog", { name: "Prepare your export" }),
    ).toBeVisible();
    await page.getByRole("button", { name: "Cancel" }).click();
    await expect(
      page.getByRole("dialog", { name: "Prepare your export" }),
    ).not.toBeVisible();
  });

  test("exports account data and confirms account deletion on desktop", async ({
    page,
  }) => {
    await page.setViewportSize({ width: 1280, height: 900 });
    await page.goto("/settings");
    await expect(page.getByRole("heading", { name: "Settings" })).toBeVisible();

    await page.getByRole("button", { name: "Export data" }).click();
    await expect(
      page.getByRole("dialog", { name: "Prepare your export" }),
    ).toBeVisible();
    await page
      .getByLabel("Export passphrase")
      .fill("a-secure-passphrase-at-least-12");
    const download = page.waitForEvent("download");
    await page
      .getByRole("button", { name: "Download encrypted export" })
      .click();
    expect((await download).suggestedFilename()).toMatch(
      /duolify-data-.*\.json/,
    );

    await page.getByRole("button", { name: "Delete account" }).click();
    await expect(
      page.getByRole("dialog", { name: "Delete your account?" }),
    ).toBeVisible();
    await page.getByLabel("Type DELETE to confirm").fill("DELETE");
    await page.getByRole("button", { name: "Delete permanently" }).click();

    await expect(page).toHaveURL(/sign-in/);
    await expect(
      page.getByRole("heading", {
        name: "Learn any topic, a little at a time.",
      }),
    ).toBeVisible();

    // Verify content disappears and session is revoked
    await page.goto("/settings");
    await expect(
      page.getByRole("heading", { name: "Session expired" }),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Sign in again" }),
    ).toBeVisible();
  });
});
