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
  cookies.filter((c) => c.name === "duolify_refresh");

test("session survives reload, typed URL and a new tab, and sign-out sticks", async ({
  page,
  context,
  request,
}) => {
  await signIn(page, `session-${crypto.randomUUID()}@example.test`, request);

  // The refresh credential is an HttpOnly, Secure, SameSite=Strict cookie scoped to /v1/auth/.
  const cookies = refreshCookies(await context.cookies());
  expect(cookies).toHaveLength(1);
  expect(cookies[0]).toMatchObject({
    httpOnly: true,
    secure: true,
    sameSite: "Strict",
    path: "/v1/auth/",
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
