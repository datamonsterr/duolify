import { expect, signUp, test, uniqueEmail } from "./stack";

test.use({ flowId: "F10" });

test(
  "F10 settings: appearance, language and analytics consent persist",
  {
    annotation: {
      type: "flow",
      description: "F10 Settings: appearance, language, analytics consent",
    },
  },
  async ({ page, flow }) => {
    await signUp(page, uniqueEmail("f10"), undefined, "/settings");
    await page.goto("/settings");
    const main = page.getByRole("main");
    await expect(
      main.getByRole("heading", { name: "Settings", level: 1 }),
    ).toBeVisible();
    // The shell also renders session-only preference controls; use the saved form's.
    const form = main.locator("form").filter({
      has: page.getByRole("button", { name: "Save settings" }),
    });
    await flow.step("settings");

    await form.getByRole("radio", { name: "Dark" }).click();
    await page.getByRole("link", { name: "Privacy and data" }).click();
    const analytics = form.getByRole("checkbox", { name: "Analytics" });
    const consentBefore = await analytics.isChecked();
    await analytics.setChecked(!consentBefore);
    await form.getByRole("button", { name: "Save settings" }).click();
    await expect(
      page.getByRole("status").filter({ hasText: "Settings saved" }),
    ).toBeVisible();
    await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
    await flow.step("dark appearance and analytics consent saved");

    // Saved on the account: a reload reads them back from the API.
    await page.reload();
    await expect(form.getByRole("checkbox", { name: "Analytics" })).toBeChecked(
      { checked: !consentBefore },
    );
    await page.getByRole("link", { name: "Look and language" }).click();
    await expect(form.getByRole("radio", { name: "Dark" })).toBeChecked();
    await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
    await flow.step("settings after reload");

    await form.getByLabel("Language").selectOption({ label: "العربية" });
    await form.getByRole("button", { name: "Save settings" }).click();
    await expect(
      page.getByRole("heading", { name: "الإعدادات", level: 1 }),
    ).toBeVisible();
    await expect(page.locator("html")).toHaveAttribute("dir", "rtl");
    await flow.step("arabic, right to left");
  },
);
