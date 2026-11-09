import { test, expect } from "@playwright/test";
import { apiBase } from "./servers";
test.use({ hasTouch: true });
test.beforeEach(async ({ page, request }) => {
  await page.route(/\/v1\/(auth\/|me(?:\/|$|\?))/, async (route) => {
    const original = route.request();
    const target = new URL(original.url());
    target.host = new URL(apiBase).host;
    const headers = original.headers();
    delete headers.origin;
    await route.fulfill({
      response: await request.fetch(target.href, {
        method: original.method(),
        headers,
        data: original.postData() ?? undefined,
      }),
    });
  });
});
for (const width of [1280, 390, 320]) {
  for (const colorScheme of ["light", "dark"] as const) {
    test(`confirmed context, clear and skip at ${width}px ${colorScheme}`, async ({
      page,
      request,
      context,
    }) => {
      await page.setViewportSize({ width, height: 1000 });
      await page.emulateMedia({ colorScheme, reducedMotion: "reduce" });
      await page.goto("/sign-in?returnTo=%2Fprofile");
      await page.getByRole("button", { name: "Get started" }).tap();
      const email = `profile-${width}-${colorScheme}@example.test`;
      await page.getByLabel("Email address").fill(email);
      await page.getByLabel("I am 16 or older").check();
      await page.getByRole("button", { name: "Send code" }).tap();
      await expect(page.getByLabel("One-time code")).toBeVisible();
      const mail = await (
        await request.get(`${apiBase}/test/mail?email=${email}`)
      ).json();
      await page.getByLabel("One-time code").fill(mail.code);
      await page.getByRole("button", { name: "Sign in", exact: true }).tap();
      await page.getByRole("link", { name: "Set up learner context" }).tap();
      await expect(
        page.getByText("AI uses your optional answers"),
      ).toBeVisible();
      await page.getByRole("button", { name: "Use form instead" }).tap();
      await page
        .getByLabel("Goal", { exact: true })
        .fill("Explain the night sky to my kids");
      await page
        .getByLabel("Background", { exact: true })
        .fill("School physics");
      await page.getByLabel("Daily minutes", { exact: true }).fill("10");
      await expect(page.getByText("Unsaved changes")).toBeVisible();
      // Explicit review keeps all fields editable, with no request until Confirm.
      let writes = 0;
      let fail = true;
      await page.route("**/v1/me/profile", async (route) => {
        if (route.request().method() === "POST") {
          writes++;
          if (fail)
            return route.fulfill({
              status: 503,
              json: { error: "Could not save profile. Retry." },
            });
        }
        await route.fallback();
      });
      await page.getByRole("button", { name: "Review summary" }).tap();
      expect(writes).toBe(0);
      await expect(page.getByLabel("Goal", { exact: true })).toHaveValue(
        "Explain the night sky to my kids",
      );
      await page.getByLabel("Topic", { exact: true }).fill("Astronomy");
      await page.getByRole("button", { name: "Confirm profile" }).tap();
      await expect(page.getByRole("alert")).toContainText("Could not save");
      await expect(page.getByLabel("Background", { exact: true })).toHaveValue(
        "School physics",
      );
      await context.setOffline(true);
      await expect(
        page.getByRole("status").filter({ hasText: "Offline" }),
      ).toBeVisible();
      await expect(
        page.getByRole("button", { name: "Retry save" }),
      ).toBeDisabled();
      await context.setOffline(false);
      fail = false;
      await page.getByRole("button", { name: "Retry save" }).tap();
      await expect(page.getByText("Setup complete")).toBeVisible();
      await page.getByRole("link", { name: "Edit profile" }).tap();
      await expect(page.getByLabel("Topic", { exact: true })).toHaveValue(
        "Astronomy",
      );
      await page.getByRole("button", { name: "Clear profile" }).tap();
      await expect(page.getByLabel("Background", { exact: true })).toHaveValue(
        "",
      );
      await page.getByRole("button", { name: "Save", exact: true }).tap();
      await expect(page.getByText("Profile saved")).toBeVisible();
      await page.reload();
      await expect(page.getByLabel("Background", { exact: true })).toHaveValue(
        "",
      );
      await expect(
        page.getByLabel("Daily minutes", { exact: true }),
      ).toHaveValue("");
      await page.getByRole("link", { name: "Set up learner context" }).tap();
      await page.getByRole("button", { name: "Use form instead" }).tap();
      await page.getByLabel("Goal", { exact: true }).fill("Discard me");
      await page.getByRole("button", { name: "Skip setup" }).tap();
      await expect(page.getByText("Setup complete")).toBeVisible();
      await page.getByRole("link", { name: "Edit profile" }).tap();
      await expect(page.getByLabel("Goal", { exact: true })).toHaveValue("");
      await page.getByLabel("Goal", { exact: true }).focus();
      await page.keyboard.press("Tab");
      await expect(page.getByLabel("Topic", { exact: true })).toBeFocused();
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth,
        ),
      ).toBe(true);
    });
  }
}
