import {
  expect,
  test,
  type APIRequestContext,
  type Page,
} from "@playwright/test";
import { sessionApi } from "./ports";

// A real browser against the real API on one origin: reload, typed URL and a new tab keep the
// learner signed in; sign-out ends it for good. No native credential-store double here.
async function signIn(page: Page, email: string, request: APIRequestContext) {
  await page.goto("/sign-in");
  await page.getByRole("button", { name: "Get started" }).click();
  await page.getByLabel("Email address").fill(email);
  await page.getByLabel("I am 16 or older").check();
  await page.getByRole("button", { name: "Send code" }).click();
  await expect(page.getByLabel("One-time code")).toBeVisible();
  const { code } = await (
    await request.get(
      `${sessionApi}/test/mail?email=${encodeURIComponent(email)}`,
    )
  ).json();
  await page.getByLabel("One-time code").fill(code);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(page).not.toHaveURL(/\/sign-in/, { timeout: 15_000 });
}

const refreshCookies = (cookies: { name: string }[]) =>
  cookies.filter((c) => c.name === "__Host-duolify_refresh");

test("session survives reload, typed URL and a new tab, and sign-out sticks", async ({
  page,
  context,
  request,
}) => {
  await signIn(page, `session-${crypto.randomUUID()}@example.test`, request);

  // The refresh credential is an HttpOnly, Secure, SameSite=Strict host-bound cookie with Path=/.
  const cookies = refreshCookies(await context.cookies());
  expect(cookies).toHaveLength(1);
  expect(cookies[0]).toMatchObject({
    httpOnly: true,
    secure: true,
    sameSite: "Strict",
    path: "/",
  });
  expect(await page.evaluate(() => document.cookie)).not.toContain(
    "duolify_refresh",
  );
  expect(
    await page.evaluate(() =>
      JSON.stringify({ ...localStorage, ...sessionStorage }),
    ),
  ).not.toMatch(/refresh/i);

  await page.reload();
  await expect(page).not.toHaveURL(/\/sign-in/);
  await expect(page.getByRole("navigation").first()).toBeVisible();

  await page.goto("/courses");
  await expect(page).toHaveURL(/\/courses$/);
  await expect(page.getByRole("heading", { name: "Courses" })).toBeVisible();

  const second = await context.newPage();
  await second.goto("/profile");
  await expect(second).toHaveURL(/\/profile$/);
  await expect(second.getByRole("button", { name: "Sign out" })).toBeVisible();
  await second.close();

  await page.goto("/profile");
  await page.getByRole("button", { name: "Sign out" }).click();
  await expect(page).toHaveURL(/\/sign-in/);
  expect(refreshCookies(await context.cookies())).toHaveLength(0);
  await page.goto("/profile");
  await expect(page).toHaveURL(/\/sign-in/);
  await page.reload();
  await expect(page).toHaveURL(/\/sign-in/);
});

test("cookie refresh responses are serialized across tabs", async ({
  page,
  context,
  request,
}) => {
  await signIn(page, `race-${crypto.randomUUID()}@example.test`, request);
  let release!: () => void;
  const held = new Promise<void>((resolve) => {
    release = resolve;
  });
  let entered!: () => void;
  const firstEntered = new Promise<void>((resolve) => {
    entered = resolve;
  });
  let requests = 0;
  await context.route("**/v1/auth/refresh", async (route) => {
    requests++;
    if (requests === 1) {
      entered();
      await held;
    }
    await route.continue();
  });
  const second = await context.newPage();
  const third = await context.newPage();
  const navSecond = second.goto("/profile");
  await firstEntered;
  const navThird = third.goto("/profile");
  // The first fetch has not processed its response; no other tab may mutate the jar.
  await expect
    .poll(() =>
      page.evaluate(
        async () =>
          (await navigator.locks.query()).pending?.filter(
            (lock) => lock.name === "duolify-auth-cookie",
          ).length,
      ),
    )
    .toBe(1);
  const whileHeld = requests;
  release();
  await Promise.all([navSecond, navThird]);
  expect(whileHeld).toBe(1);
  await expect(second.getByRole("button", { name: "Sign out" })).toBeVisible();
  await expect(third.getByRole("button", { name: "Sign out" })).toBeVisible();
  await third.reload();
  await expect(third.getByRole("button", { name: "Sign out" })).toBeVisible();
});

test("a sibling domain cannot plant or shadow the host refresh cookie", async ({
  page,
  context,
}) => {
  await context.route("https://*.duolify.test/**", (route) =>
    route.fulfill({
      contentType: "text/html",
      body: "<title>Cookie prefix protocol fixture</title>",
      ...(new URL(route.request().url()).hostname === "app.duolify.test"
        ? {
            headers: {
              "set-cookie":
                "__Host-duolify_refresh=legitimate; Path=/; Secure; HttpOnly; SameSite=Strict",
            },
          }
        : {}),
    }),
  );
  await page.goto("https://app.duolify.test/");
  await page.goto("https://sibling.duolify.test/");
  await page.evaluate(() => {
    document.cookie =
      "duolify_refresh=attacker; Domain=duolify.test; Path=/v1/auth/; Secure";
    document.cookie =
      "__Host-duolify_refresh=attacker; Domain=duolify.test; Path=/; Secure";
    document.cookie =
      "__Host-duolify_refresh=attacker; Domain=duolify.test; Path=/v1/auth/; Secure";
  });
  const cookies = await context.cookies(
    "https://app.duolify.test/v1/auth/refresh",
  );
  expect(cookies.filter((c) => c.name === "duolify_refresh")).toHaveLength(1);
  expect(refreshCookies(cookies)).toEqual([
    expect.objectContaining({
      value: "legitimate",
      domain: "app.duolify.test",
      path: "/",
    }),
  ]);
});
