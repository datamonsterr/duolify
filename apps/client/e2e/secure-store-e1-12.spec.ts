import { test, expect } from "@playwright/test";

test("browser fallback ignores persisted tokens and starts signed out", async ({
  page,
}) => {
  await page.addInitScript(() => {
    sessionStorage.setItem(
      "duolify.auth",
      JSON.stringify({
        access_token: "stale",
        refresh_token: "stale",
        expires_in: 900,
      }),
    );
  });
  await page.route("**/v1/me", (route) =>
    route.fulfill({
      json: {
        id: "56d4e614-7665-4f14-b83e-8b5e9bed384a",
        display_name: "Learner",
        locale: "en",
        time_zone: "UTC",
        analytics_opt_out: false,
      },
    }),
  );
  await page.goto("/profile");
  await expect(
    page.getByRole("heading", { name: "Learn any topic, a little at a time." }),
  ).toBeVisible();
});

test("mobile platform store double persists session across restart and clears on sign-out", async ({
  page,
}) => {
  await page.addInitScript(() => {
    const key = "duolify.platform-secure-store-double";
    Object.defineProperty(window, "__TAURI_INTERNALS__", {
      configurable: true,
      value: {
        invoke: async (command: string, args?: { value?: string | null }) => {
          if (command === "read_credentials") {
            return localStorage.getItem(key);
          }
          if (command === "write_credentials") {
            if (args?.value) localStorage.setItem(key, args.value);
            else localStorage.removeItem(key);
            return;
          }
          throw new Error(`Unexpected native command: ${command}`);
        },
      },
    });
    Object.defineProperty(window, "isTauri", {
      configurable: true,
      value: true,
    });
  });
  await page.route(/\/v1\/(auth\/|me(?:$|\?))/, async (route) => {
    const { pathname } = new URL(route.request().url());
    if (pathname.endsWith("/auth/code")) {
      await route.fulfill({ status: 202, json: {} });
    } else if (pathname.endsWith("/auth/verify")) {
      await route.fulfill({
        status: 200,
        json: {
          access_token: "access",
          refresh_token: "refresh",
          expires_in: 900,
        },
      });
    } else if (pathname.endsWith("/auth/logout")) {
      await route.fulfill({ status: 204 });
    } else {
      await route.fulfill({
        status: 200,
        json: {
          id: "56d4e614-7665-4f14-b83e-8b5e9bed384a",
          display_name: "Learner",
          locale: "en",
          time_zone: "UTC",
          background: "",
          analytics_opt_out: false,
        },
      });
    }
  });

  await page.goto("/sign-in?returnTo=%2Fprofile");
  await page.getByRole("button", { name: "Get started" }).click();
  await page.getByLabel("Email address").fill("mobile@example.test");
  await page.getByLabel("I am 16 or older").check();
  await page.getByRole("button", { name: "Send code" }).click();
  await page.getByLabel("One-time code").fill("123456");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect
    .poll(() =>
      page.evaluate(() =>
        localStorage.getItem("duolify.platform-secure-store-double"),
      ),
    )
    .toContain("refresh_token");
  expect(
    await page.evaluate(() => sessionStorage.getItem("duolify.auth")),
  ).toBeNull();

  await page.goto("/profile");
  await expect(page.getByText("Signed in as Learner")).toBeVisible();
  await page.reload();
  await expect(page.getByText("Signed in as Learner")).toBeVisible();
  await page.getByRole("button", { name: "Sign out" }).click();
  await expect(
    page.getByRole("heading", { name: "Welcome back" }),
  ).toBeVisible();
  expect(
    await page.evaluate(() =>
      localStorage.getItem("duolify.platform-secure-store-double"),
    ),
  ).toBeNull();
});
