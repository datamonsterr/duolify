import {
  expect,
  noteShellDefects,
  signInCode,
  signUp,
  stackURL,
  test,
  uniqueEmail,
} from "./stack";

test.use({ flowId: "F1" });

test(
  "F1 email-code sign-up, then sign-in on a new device, lands in the app",
  {
    annotation: {
      type: "flow",
      description: "F1 Email-code sign-up and sign-in",
    },
  },
  async ({ page, browser, flow }) => {
    const email = uniqueEmail("f1");
    await signUp(page, email, flow);
    await expect(page.getByRole("navigation").first()).toBeVisible();
    await noteShellDefects(page, flow);
    await flow.step("signed up, landed in app");

    // Same account on a second device: a fresh browser context with no session.
    const device = await browser.newContext({ baseURL: stackURL });
    const second = await device.newPage();
    try {
      await second.goto("/profile");
      await expect(second).toHaveURL(/\/sign-in/);
      await flow.step("new device redirected to sign-in", second);
      await second
        .getByRole("button", { name: "Sign in", exact: true })
        .click();
      await expect(
        second.getByRole("heading", { name: "Welcome back" }),
      ).toBeVisible();
      await second.getByLabel("Email address").fill(email);
      await second.getByLabel("Email address").press("Enter");
      await second.getByLabel("I am 16 or older").check();
      await second.getByRole("button", { name: "Send code" }).click();
      await expect(second.getByLabel("One-time code")).toBeVisible();
      await second.getByLabel("One-time code").fill(await signInCode(email));
      await flow.step("returning sign-in code entered", second);
      await second
        .getByRole("button", { name: "Sign in", exact: true })
        .click();
      await expect(second).not.toHaveURL(/\/sign-in/, { timeout: 15_000 });
      await expect(second.getByRole("navigation").first()).toBeVisible();
      await noteShellDefects(second, flow);
      await flow.step("returning learner landed in app", second);
    } finally {
      await device.close();
    }
  },
);
