import { expect, test } from "@playwright/test";

test.use({ hasTouch: true });

for (const width of [1280, 390, 320]) {
  for (const mode of ["light", "dark"] as const) {
    test(`Apple browser sign-in and cancellation recovery at ${width}px ${mode}`, async ({
      page,
      context,
    }) => {
      await page.setViewportSize({ width, height: 900 });
      await page.emulateMedia({ colorScheme: mode, reducedMotion: "reduce" });
      await page.addInitScript(() => {
        const listeners = new Set<
          (result: { state: string; code: string } | null) => void
        >();
        (
          window as typeof window & { __DUOLIFY_SSO_BRIDGE__?: unknown }
        ).__DUOLIFY_SSO_BRIDGE__ = {
          openSystemBrowser: async () => undefined,
          listen: (
            listener: (result: { state: string; code: string } | null) => void,
          ) => {
            listeners.add(listener);
            return () => listeners.delete(listener);
          },
          cancel: () => listeners.forEach((listener) => listener(null)),
        };
      });
      await page.route("**/v1/auth/sso/config", (route) =>
        route.fulfill({
          json: { enabled: true, publishable_key: "pk_test_fixture" },
        }),
      );
      await page.route("**/v1/auth/sso/start", (route) =>
        route.fulfill({
          json: {
            state: "s".repeat(43),
          },
        }),
      );
      await page.goto("/sign-in?returnTo=%2Fprofile");
      if (width < 480) {
        await page.evaluate(() => {
          document.documentElement.style.fontSize = "200%";
        });
      }
      const apple = page.getByRole("button", { name: "Continue with Apple" });
      await expect(apple).toBeVisible();
      await page.getByLabel("I am 16 or older").check();
      await context.setOffline(true);
      await expect(apple).toBeDisabled();
      await context.setOffline(false);
      await expect(apple).toBeEnabled();
      if (width < 480) await apple.tap();
      else {
        await apple.focus();
        await page.keyboard.press("Enter");
      }
      await expect(page.getByRole("status")).toContainText(
        "opens in your browser",
      );
      await page.evaluate(() => {
        const bridge = (
          window as typeof window & {
            __DUOLIFY_SSO_BRIDGE__: { cancel: () => void };
          }
        ).__DUOLIFY_SSO_BRIDGE__;
        bridge.cancel();
      });
      await expect(page.getByRole("status")).toContainText("cancelled");
      await expect(apple).toBeFocused();
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

test("Apple sign-in error preserves destination and supports retry", async ({
  page,
}) => {
  let starts = 0;
  await page.route("**/v1/auth/sso/config", (route) =>
    route.fulfill({
      json: { enabled: true, publishable_key: "pk_test_fixture" },
    }),
  );
  await page.route("**/v1/auth/sso/start", (route) => {
    starts++;
    if (starts === 1)
      return route.fulfill({
        status: 503,
        json: { error: "Apple sign-in is unavailable. Please retry." },
      });
    return route.fulfill({
      json: {
        state: "s".repeat(43),
      },
    });
  });
  await page.addInitScript(() => {
    (
      window as typeof window & { __DUOLIFY_SSO_BRIDGE__?: unknown }
    ).__DUOLIFY_SSO_BRIDGE__ = {
      openSystemBrowser: async () => undefined,
      listen: async () => () => undefined,
    };
  });
  await page.goto("/sign-in?returnTo=%2Flesson%2Fsky-changes");
  await page.getByLabel("I am 16 or older").check();
  await page.getByRole("button", { name: "Continue with Apple" }).click();
  await expect(page.getByRole("alert")).toContainText("unavailable");
  await page.getByRole("button", { name: "Retry sign-in" }).click();
  await expect(page.getByRole("status")).toContainText("opens in your browser");
  await expect(page).toHaveURL(/returnTo=%2Flesson%2Fsky-changes/);
});

test("returning Apple subject completes when profile claims are omitted", async ({
  page,
}) => {
  await page.route("**/v1/auth/sso/config", (route) =>
    route.fulfill({
      json: { enabled: true, publishable_key: "pk_test_fixture" },
    }),
  );
  await page.route("**/v1/auth/sso/start", (route) =>
    route.fulfill({
      json: {
        state: "s".repeat(43),
      },
    }),
  );
  await page.addInitScript(() => {
    let listener:
      ((result: { state: string; code: string } | null) => void) | undefined;
    (
      window as typeof window & {
        __DUOLIFY_SSO_BRIDGE__: {
          openSystemBrowser: () => Promise<void>;
          listen: (
            cb: (result: { state: string; code: string } | null) => void,
          ) => Promise<() => void>;
          complete: () => void;
        };
      }
    ).__DUOLIFY_SSO_BRIDGE__ = {
      openSystemBrowser: async () => undefined,
      listen: async (cb) => {
        listener = cb;
        return () => undefined;
      },
      complete: () =>
        listener?.({ state: "s".repeat(43), code: "c".repeat(43) }),
    };
  });
  await page.route("**/v1/auth/sso/complete", async (route) => {
    const body = route.request().postDataJSON() as Record<string, unknown>;
    expect(body).toMatchObject({
      state: "s".repeat(43),
      code: "c".repeat(43),
      redirect_uri: "duolify://sso/callback",
    });
    expect(body.code_verifier).toMatch(/^[A-Za-z0-9_-]{43}$/);
    await route.fulfill({
      json: {
        access_token: "access-fixture",
        refresh_token: "refresh-fixture",
        expires_in: 900,
      },
    });
  });
  await page.route("**/v1/me", (route) =>
    route.fulfill({
      json: {
        id: "56d4e614-7665-4f14-b83e-8b5e9bed384a",
        display_name: "First Login Learner",
        locale: "en",
        time_zone: "UTC",
        background: "",
        analytics_opt_out: false,
      },
    }),
  );
  await page.goto("/sign-in");
  await page.getByLabel("I am 16 or older").check();
  await page.getByRole("button", { name: "Continue with Apple" }).click();
  await expect(
    page.getByRole("button", { name: "Cancel sign-in" }),
  ).toBeVisible();
  await page.evaluate(() => {
    const bridge = (
      window as typeof window & {
        __DUOLIFY_SSO_BRIDGE__: { complete: () => void };
      }
    ).__DUOLIFY_SSO_BRIDGE__;
    bridge.complete();
  });
  await expect(page).toHaveURL(/\/$/);
  await expect(
    page.getByText("Signed in as First Login Learner"),
  ).toBeVisible();
});
