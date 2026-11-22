import { expect, signUp, test, uniqueEmail } from "./stack";

test.use({ flowId: "F1c" });

// The web build keeps its refresh credential in an HttpOnly cookie, so a full page load
// (reload, typed URL, new tab, the SSO redirect returning to the tab) restores the session.
test(
  "F1c web session survives reload, typed URL and a new tab; sign-out sticks",
  {
    annotation: {
      type: "flow",
      description: "F1c Signed-in web session persists across full page loads",
    },
  },
  async ({ page, context, flow }) => {
    await signUp(page, uniqueEmail("f1c"), flow);
    await expect(page.getByRole("navigation").first()).toBeVisible();
    await flow.step("signed in");

    await page.reload();
    await expect(page).not.toHaveURL(/\/sign-in/);
    await expect(page.getByRole("navigation").first()).toBeVisible();
    await flow.step("reload keeps the session");

    await page.goto("/courses");
    await expect(page).not.toHaveURL(/\/sign-in/);
    await expect(page.getByRole("heading", { name: "Courses" })).toBeVisible();
    await flow.step("typed URL keeps the session");

    const second = await context.newPage();
    await second.goto("/profile");
    await expect(second).not.toHaveURL(/\/sign-in/);
    await expect(
      second.getByRole("button", { name: "Sign out" }),
    ).toBeVisible();
    await flow.step("new tab keeps the session", second);
    await second.close();

    await page.goto("/profile");
    await page.getByRole("button", { name: "Sign out" }).click();
    await expect(page).toHaveURL(/\/sign-in/);
    await page.goto("/profile");
    await expect(page).toHaveURL(/\/sign-in/);
    await page.reload();
    await expect(page).toHaveURL(/\/sign-in/);
    await flow.step("signed out stays signed out after reload");
  },
);
