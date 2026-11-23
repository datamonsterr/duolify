import { readFileSync } from "node:fs";
import { expect, signUp, test, uniqueEmail } from "./stack";

test.use({ flowId: "F11" });

test(
  "F11 account: export my data, then delete the account",
  {
    annotation: {
      type: "flow",
      description: "F11 Account: export data, delete account",
    },
  },
  async ({ page, flow }) => {
    const email = uniqueEmail("f11");
    await signUp(page, email, undefined, "/settings");
    await page.goto("/settings#privacy");
    const account = page.getByRole("region", {
      name: "Account data and privacy",
    });
    await expect(account).toBeVisible();
    await flow.step("account section");

    await account.getByRole("button", { name: "Export data" }).click();
    const exportDialog = page.getByRole("dialog", {
      name: "Prepare your export",
    });
    await exportDialog
      .getByLabel("Export passphrase")
      .fill("e2e stack passphrase 42");
    await flow.step("export passphrase");
    const download = page.waitForEvent("download");
    await exportDialog
      .getByRole("button", { name: "Download encrypted export" })
      .click();
    const file = await download;
    expect(file.suggestedFilename()).toMatch(
      /^duolify-data-\d{4}-\d{2}-\d{2}\.json$/,
    );
    const saved = await file.path();
    const body = readFileSync(saved, "utf8");
    // Encrypted on the device: the export must not carry the account email in clear text.
    expect(body.length).toBeGreaterThan(0);
    expect(body).not.toContain(email);
    await expect(page.getByText(/Encrypted export downloaded\./)).toBeVisible();
    await flow.step("encrypted export downloaded");

    await account.getByRole("button", { name: "Delete account" }).click();
    const deleteDialog = page.getByRole("dialog", {
      name: "Delete your account?",
    });
    const confirm = deleteDialog.getByRole("button", {
      name: "Delete permanently",
    });
    await expect(confirm).toBeDisabled();
    await deleteDialog.getByLabel("Type DELETE to confirm").fill("DELETE");
    await expect(confirm).toBeEnabled();
    await flow.step("delete confirmation");
    await confirm.click();
    await expect(page).toHaveURL(/\/sign-in/);
    await flow.step("signed out after deletion");
    // The session is gone: a protected page sends the visitor back to sign-in.
    await page.goto("/settings");
    await expect(page).toHaveURL(/\/sign-in/);
  },
);
