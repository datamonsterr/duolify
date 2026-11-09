import {
  test,
  expect,
  type Page,
  type APIRequestContext,
} from "@playwright/test";
import { apiBase, resetAuthRateLimits } from "./servers";
test.use({ hasTouch: true });
test.beforeEach(async ({ request }) => {
  await resetAuthRateLimits(request);
});
async function login(request: APIRequestContext, email: string) {
  const body = { email, age_confirmed: true };
  expect(
    (await request.post(`${apiBase}/v1/auth/code`, { data: body })).status(),
  ).toBe(202);
  const { code } = await (
    await request.get(`${apiBase}/test/mail?email=${email}`)
  ).json();
  const response = await request.post(`${apiBase}/v1/auth/verify`, {
    data: { ...body, code },
  });
  expect(response.status()).toBe(200);
  return response.json();
}
async function bridge(page: Page, request: APIRequestContext) {
  await page.route(/\/v1\/(auth\/|me(?:$|\?))/, async (route) => {
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
}
for (const width of [1280, 320]) {
  for (const colorScheme of ["light", "dark"] as const) {
    test(`rotate and revoke devices ${width} ${colorScheme}`, async ({
      page,
      request,
    }) => {
      const email = `sessions-${width}-${colorScheme}@example.test`;
      const first = await login(request, email);
      const expiredAccess = await request.post(
        `${apiBase}/test/expire-access`,
        { data: { access_token: first.access_token } },
      );
      expect(expiredAccess.status()).toBe(200);
      first.access_token = (await expiredAccess.json()).access_token;
      const other = await login(request, email);
      await bridge(page, request);
      await page.setViewportSize({ width, height: 900 });
      await page.emulateMedia({ colorScheme, reducedMotion: "reduce" });
      await page.addInitScript((tokens) => {
        if (!sessionStorage.getItem("session-fixture-seeded")) {
          sessionStorage.setItem("duolify.auth", JSON.stringify(tokens));
          sessionStorage.setItem("session-fixture-seeded", "true");
        }
      }, first);
      await page.goto("/settings/sessions");
      await expect(
        page.getByRole("heading", { name: "Sessions", exact: true }),
      ).toBeVisible();
      await expect(
        page.getByText("This device", { exact: true }),
      ).toBeVisible();
      await expect
        .poll(() =>
          page.evaluate(
            () =>
              JSON.parse(sessionStorage.getItem("duolify.auth")!).refresh_token,
          ),
        )
        .not.toBe(first.refresh_token);
      await page
        .getByRole("button", { name: "Sign out device", exact: true })
        .tap();
      await expect(
        page.getByRole("button", { name: "Sign out device", exact: true }),
      ).toHaveCount(0);
      expect(
        (
          await request.get(`${apiBase}/v1/me`, {
            headers: { Authorization: `Bearer ${other.access_token}` },
          })
        ).status(),
      ).toBe(401);
      await page.getByRole("button", { name: "Sign out all devices" }).focus();
      await page.keyboard.press("Enter");
      await expect(page).toHaveURL(/sign-in/);
      expect(
        await page.evaluate(() => sessionStorage.getItem("duolify.auth")),
      ).toBeNull();
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth,
        ),
      ).toBe(true);
    });
  }
}

for (const width of [1280, 320]) {
  test(`session loading, offline, empty and expanded content at ${width}`, async ({
    page,
    request,
    context,
  }) => {
    const tokens = await login(request, `states-${width}@example.test`);
    await bridge(page, request);
    await page.setViewportSize({ width, height: 900 });
    await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
    await page.addInitScript((value) => {
      if (!sessionStorage.getItem("session-fixture-seeded")) {
        sessionStorage.setItem("duolify.auth", JSON.stringify(value));
        sessionStorage.setItem("session-fixture-seeded", "true");
      }
    }, tokens);
    let release!: () => void;
    const held = new Promise<void>((resolve) => {
      release = resolve;
    });
    let state: "loading" | "error" | "empty" | "ready" = "loading";
    await page.route("**/v1/auth/sessions", async (route) => {
      if (state === "loading") await held;
      if (state === "error") await route.abort();
      else if (state === "empty") await route.fulfill({ json: [] });
      else await route.fallback();
    });
    await page.goto("/settings/sessions");
    await expect(page.getByRole("status")).toContainText("Updating sessions");
    state = "error";
    release();
    await expect(page.getByRole("alert")).toContainText("Connect and retry");
    expect(
      await page.evaluate(() => sessionStorage.getItem("duolify.auth")),
    ).not.toBeNull();
    await context.setOffline(true);
    await page.getByRole("button", { name: "Retry" }).tap();
    await expect(page.getByRole("alert")).toContainText("Connect and retry");
    await context.setOffline(false);
    state = "empty";
    await page.getByRole("button", { name: "Retry" }).focus();
    await page.keyboard.press("Enter");
    await expect(page.getByText("No active sessions.")).toBeVisible();
    await page.reload();
    state = "ready";
    await page.reload();
    await expect(page.getByText("This device", { exact: true })).toBeVisible();
    await page.evaluate(() => {
      document.documentElement.style.fontSize = "200%";
    });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    await page.getByRole("button", { name: "Sign out this device" }).tap();
    await expect(page).toHaveURL(/sign-in/);
  });
}
