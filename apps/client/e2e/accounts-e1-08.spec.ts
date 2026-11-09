import { expect, test } from "@playwright/test";

test.use({ hasTouch: true });

for (const width of [1280, 390, 320]) {
  for (const mode of ["light", "dark"] as const) {
    test(`Google browser sign-in and cancellation recovery at ${width}px ${mode}`, async ({
      page,
      context,
    }) => {
      await page.setViewportSize({ width, height: 900 });
      await page.emulateMedia({ colorScheme: mode, reducedMotion: "reduce" });
      await page.addInitScript(() => {
        const listeners = new Set<
          (result: { code: string; state: string } | null) => void
        >();
        (
          window as typeof window & { __DUOLIFY_OAUTH_BRIDGE__?: unknown }
        ).__DUOLIFY_OAUTH_BRIDGE__ = {
          openSystemBrowser: async () => undefined,
          listen: (
            listener: (result: { code: string; state: string } | null) => void,
          ) => {
            listeners.add(listener);
            return () => listeners.delete(listener);
          },
          cancel: () => listeners.forEach((listener) => listener(null)),
        };
      });
      await page.route("**/v1/auth/google/start", (route) =>
        route.fulfill({
          json: {
            state: "state-fixture",
            nonce: "n".repeat(32),
            authorization_url:
              "https://accounts.google.test/authorize?state=state-fixture",
          },
        }),
      );
      await page.goto("/sign-in?returnTo=%2Fprofile");
      if (width < 480) {
        await page.evaluate(() => {
          document.documentElement.style.fontSize = "200%";
        });
      }
      const google = page.getByRole("button", { name: "Continue with Google" });
      await expect(google).toBeVisible();
      await page.getByLabel("I am 16 or older").check();
      await context.setOffline(true);
      await expect(google).toBeDisabled();
      await context.setOffline(false);
      await expect(google).toBeEnabled();
      if (width < 480) await google.tap();
      else {
        await google.focus();
        await page.keyboard.press("Enter");
      }
      await expect(page.getByRole("status")).toContainText(
        "opens in your browser",
      );
      await page.evaluate(() => {
        const bridge = (
          window as typeof window & {
            __DUOLIFY_OAUTH_BRIDGE__: { cancel: () => void };
          }
        ).__DUOLIFY_OAUTH_BRIDGE__;
        bridge.cancel();
      });
      await expect(page.getByRole("status")).toContainText("cancelled");
      await expect(google).toBeFocused();
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

test("Google sign-in error preserves destination and supports retry", async ({
  page,
}) => {
  let starts = 0;
  await page.route("**/v1/auth/google/start", (route) => {
    starts++;
    if (starts === 1)
      return route.fulfill({
        status: 503,
        json: { error: "Google sign-in is unavailable. Please retry." },
      });
    return route.fulfill({
      json: {
        state: "state-fixture",
        nonce: "n".repeat(32),
        authorization_url:
          "https://accounts.google.test/authorize?state=state-fixture",
      },
    });
  });
  await page.addInitScript(() => {
    (
      window as typeof window & { __DUOLIFY_OAUTH_BRIDGE__?: unknown }
    ).__DUOLIFY_OAUTH_BRIDGE__ = {
      openSystemBrowser: async () => undefined,
      listen: async () => () => undefined,
    };
  });
  await page.goto("/sign-in?returnTo=%2Flesson%2Fsky-changes");
  await page.getByLabel("I am 16 or older").check();
  await page.getByRole("button", { name: "Continue with Google" }).click();
  await expect(page.getByRole("alert")).toContainText("unavailable");
  await page.getByRole("button", { name: "Retry sign-in" }).click();
  await expect(page.getByRole("status")).toContainText("opens in your browser");
  await expect(page).toHaveURL(/returnTo=%2Flesson%2Fsky-changes/);
});

test("Google callback completes sign-in with transaction state", async ({
  page,
}) => {
  await page.route("**/v1/auth/google/start", (route) =>
    route.fulfill({
      json: {
        state: "state-fixture",
        nonce: "n".repeat(32),
        authorization_url:
          "https://accounts.google.test/authorize?state=state-fixture",
      },
    }),
  );
  await page.route("**/v1/auth/google/complete", (route) =>
    route.fulfill({
      json: {
        access_token: "access-fixture",
        refresh_token: "refresh-fixture",
        expires_in: 900,
      },
    }),
  );
  await page.route("**/v1/me", (route) =>
    route.fulfill({
      json: {
        id: "56d4e614-7665-4f14-b83e-8b5e9bed384a",
        display_name: "Learner",
        locale: "en",
        time_zone: "UTC",
        background: "",
        analytics_opt_out: false,
      },
    }),
  );
  await page.addInitScript(() => {
    let listener:
      ((result: { code: string; state: string } | null) => void) | undefined;
    (
      window as typeof window & { __DUOLIFY_OAUTH_BRIDGE__?: unknown }
    ).__DUOLIFY_OAUTH_BRIDGE__ = {
      openSystemBrowser: async () => undefined,
      listen: async (
        callback: (result: { code: string; state: string } | null) => void,
      ) => {
        listener = callback;
        return () => undefined;
      },
      complete: () =>
        listener?.({ code: "fixture-code", state: "state-fixture" }),
    };
  });
  await page.goto("/sign-in");
  await page.getByLabel("I am 16 or older").check();
  await page.getByRole("button", { name: "Continue with Google" }).click();
  await page.evaluate(() => {
    const bridge = (
      window as typeof window & {
        __DUOLIFY_OAUTH_BRIDGE__: { complete: () => void };
      }
    ).__DUOLIFY_OAUTH_BRIDGE__;
    bridge.complete();
  });
  await expect(page).toHaveURL(/\/profile$/);
  await expect(page.getByText("Signed in as Learner")).toBeVisible();
});
