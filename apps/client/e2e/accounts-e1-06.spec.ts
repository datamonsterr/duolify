import { test, expect } from "./accounts-fixture";
test.use({ hasTouch: true });
test("settings and profile drafts persist", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/settings");
  await expect(page.getByRole("heading", { name: "Settings" })).toBeVisible();
  await page.getByLabel("Appearance").selectOption("dark");
  await page.getByRole("button", { name: "Save settings" }).tap();
  await expect(page.getByText("Settings saved")).toBeVisible();
  await page.reload();
  await expect(page.getByLabel("Appearance")).toHaveValue("dark");
  await page.getByLabel("Analytics").uncheck();
  await page.getByRole("button", { name: "Save settings" }).tap();
  await expect(page.getByText("Settings saved")).toBeVisible();
  await expect(page.getByLabel("Analytics")).not.toBeChecked();
  await page.reload();
  await expect(page.getByLabel("Analytics")).not.toBeChecked();
  await page.getByRole("link", { name: "Profile" }).tap();
  await page
    .getByLabel("Goal", { exact: true })
    .fill("Unconfirmed profile draft");
  await page.getByRole("button", { name: "Update by chat" }).tap();
  await page.getByRole("button", { name: "Close chat" }).tap();
  await expect(page.getByLabel("Goal", { exact: true })).toHaveValue(
    "Unconfirmed profile draft",
  );
});
