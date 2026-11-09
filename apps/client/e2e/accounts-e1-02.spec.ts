import { apiBase } from "./servers";
import { test, expect } from "@playwright/test";
test.use({ hasTouch: true });
test.beforeEach(async ({ page, request }) => {
  await page.route(/\/v1\/(auth\/|me(?:$|\?))/, async (route) => {
    const original = route.request();
    const target = new URL(original.url());
    const headers = original.headers();
    delete headers.origin;
    target.host = new URL(apiBase).host;
    await route.fulfill({
      response: await request.fetch(target.href, {
        method: original.method(),
        headers,
        data: original.postData() ?? undefined,
      }),
    });
  });
});

for (const width of [1280, 390]) {
  for (const mode of ["light", "dark"] as const) {
    test(`email sign-in and recovery at ${width}px ${mode}`, async ({
      page,
      request,
      context,
    }) => {
      await page.setViewportSize({ width, height: 900 });
      await page.emulateMedia({ colorScheme: mode, reducedMotion: "reduce" });
      await page.goto("/sign-in?returnTo=%2Fprofile");
      await expect(
        page.getByRole("heading", {
          name: "Learn any topic, a little at a time.",
        }),
      ).toBeVisible();
      await page
        .getByRole("button", { name: "I already have an account" })
        .click();
      const email = `learner-${width}-${mode}@example.test`;
      await page.getByLabel("Email address").fill(email);
      await page.getByLabel("I am 16 or older").check();
      await page.getByRole("button", { name: "Send code" }).click();
      await expect(page.getByLabel("One-time code")).toBeFocused();
      await page.getByLabel("One-time code").fill("000000");
      await page.getByRole("button", { name: "Sign in", exact: true }).click();
      await expect(page.getByRole("alert")).toContainText("invalid or expired");
      const sink = await request.get(
        `${apiBase}/test/mail?email=${encodeURIComponent(email)}`,
      );
      const { code } = await sink.json();
      await page.getByLabel("One-time code").fill(code);
      await page.keyboard.press("Enter");
      await expect(page).toHaveURL(/\/profile$/);
      await expect(page.getByText("Signed in as Learner")).toBeVisible();
      const me = await page
        .locator("[data-learner-id]")
        .getAttribute("data-learner-id");
      expect(me).toMatch(/^[a-f0-9-]{36}$/);
      expect(
        await page.evaluate(() => Object.keys(localStorage)),
      ).not.toContain("tokens");
      await page.getByRole("button", { name: "Sign out" }).click();
      await expect(
        page.getByRole("heading", { name: "Welcome back" }),
      ).toBeVisible();
      await context.setOffline(true);
      await expect(page.getByRole("status")).toContainText("Offline");
      await context.setOffline(false);
      await page.getByRole("button", { name: "Back to welcome" }).click();
      await expect(
        page.getByRole("button", { name: "Get started" }),
      ).toBeVisible();
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth,
        ),
      ).toBe(true);
    });
  }
}

test("expired code and unsafe destination preserve recovery", async ({
  page,
  request,
}) => {
  await page.goto("/sign-in?returnTo=https%3A%2F%2Fevil.test");
  await page.getByRole("button", { name: "Get started" }).click();
  await page.getByLabel("Email address").fill("expired@example.test");
  await page.getByLabel("I am 16 or older").check();
  await page.getByRole("button", { name: "Send code" }).click();
  await expect(page.getByLabel("One-time code")).toBeVisible();
  const mail = await (
    await request.get(apiBase + "/test/mail?email=expired@example.test")
  ).json();
  await request.post(apiBase + "/test/expire", {
    data: { email: "expired@example.test" },
  });
  await page.getByLabel("One-time code").fill(mail.code);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("invalid or expired");
  await page.getByRole("button", { name: "Resend code" }).click();
  await expect(page.getByLabel("One-time code")).toHaveValue("");
  const fresh = await (
    await request.get(apiBase + "/test/mail?email=expired@example.test")
  ).json();
  await page.getByLabel("One-time code").fill(fresh.code);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(page).toHaveURL(/\/profile$/);
});

for (const width of [1280, 320]) {
  test(`loading, retry, long email and cancel focus at ${width}px`, async ({
    page,
    context,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    await page.emulateMedia({ reducedMotion: "reduce" });
    let release!: () => void;
    const held = new Promise<void>((resolve) => {
      release = resolve;
    });
    let fail = true;
    await page.route("**/v1/auth/code", async (route) => {
      if (fail) {
        await held;
        await route.fulfill({
          status: 503,
          json: { error: "Sign-in is unavailable. Please retry." },
        });
      } else await route.fallback();
    });
    await page.goto("/sign-in?returnTo=%2Flesson%2Fsky-changes");
    await page.getByRole("button", { name: "Get started" }).tap();
    const email =
      `a-very-long-learner-address-${width}-`.repeat(6) + "@example.test";
    await page.getByLabel("Email address").fill(email);
    await page.getByLabel("I am 16 or older").check();
    await page.getByRole("button", { name: "Send code" }).tap();
    await expect(
      page.getByRole("button", { name: "Preparing…" }),
    ).toBeDisabled();
    release();
    await expect(page.getByRole("alert")).toContainText("unavailable");
    await expect(page.getByLabel("Email address")).toHaveValue(email);
    await context.setOffline(true);
    await expect(
      page.getByRole("button", { name: "Retry sign-in" }),
    ).toBeDisabled();
    await context.setOffline(false);
    fail = false;
    await page.getByRole("button", { name: "Retry sign-in" }).tap();
    await expect(page.getByLabel("One-time code")).toBeFocused();
    await page.evaluate(() => {
      document.documentElement.style.fontSize = "200%";
    });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    await page.getByRole("button", { name: "Change email" }).tap();
    await expect(page.getByLabel("Email address")).toHaveValue(email);
    await page.getByRole("button", { name: "Back to welcome" }).tap();
    await expect(
      page.getByRole("button", { name: "Get started" }),
    ).toBeFocused();
    await expect(page).toHaveURL(/returnTo=%2Flesson%2Fsky-changes/);
    await page.getByRole("button", { name: "Privacy", exact: true }).tap();
    await expect(page.getByRole("region", { name: "Privacy" })).toContainText(
      "never sent to analytics",
    );
    await page.getByRole("button", { name: "Close Privacy" }).tap();
    await page.getByRole("button", { name: "Analytics preference" }).tap();
    await page.getByLabel("Share anonymous product analytics").uncheck();
    expect(
      await page.evaluate(() =>
        localStorage.getItem("duolify.analytics-opt-out"),
      ),
    ).toBe("true");
  });
}
