import { test, expect, resetTokens } from "./accounts-fixture";

// Export and deletion as chats (flag `rest`, default on). The old dialogs are covered by e1-07.
test.afterEach(() => {
  resetTokens();
});
for (const [width, height] of [
  [390, 844],
  [844, 390],
  [1280, 800],
] as const) {
  test(`export then delete as chats at ${width}x${height}`, async ({
    page,
  }, info) => {
    await page.setViewportSize({ width, height });
    await page.goto("/settings");
    await page.getByRole("button", { name: "Export data" }).click();
    const dialog = page.getByRole("dialog", { name: "Export data" });
    const pass = dialog.getByLabel("Export passphrase");
    await expect(pass).toHaveAttribute("type", "password");
    await expect(pass).toHaveAttribute("autocomplete", "new-password");
    await pass.fill("short");
    await pass.press("Enter");
    await expect(dialog.getByRole("alert")).toBeVisible();
    await page.screenshot({ path: info.outputPath(`${width}-export.png`) });
    await pass.fill("a-secure-passphrase-at-least-12");
    await pass.press("Enter");
    const download = page.waitForEvent("download");
    await dialog.getByRole("button", { name: "Download" }).click();
    expect((await download).suggestedFilename()).toMatch(
      /duolify-data-.*\.json/,
    );
    await expect(page.getByText(/Encrypted export downloaded/)).toBeVisible();
    await dialog.getByRole("status").getByRole("button").click();

    await page.getByRole("button", { name: "Delete account" }).click();
    const del = page.getByRole("dialog", { name: "Delete account" });
    await expect(del.getByRole("textbox")).toHaveCount(0);
    await page.screenshot({ path: info.outputPath(`${width}-delete.png`) });
    await del.getByRole("button", { name: "Cancel" }).click();
    await expect(del).toHaveCount(0);
    await page.getByRole("button", { name: "Delete account" }).click();
    await page
      .getByRole("dialog", { name: "Delete account" })
      .getByRole("button", { name: "Delete forever" })
      .click();
    await expect(page).toHaveURL(/sign-in/);
  });
}
