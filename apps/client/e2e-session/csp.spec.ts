import {
  expect,
  test,
  type APIRequestContext,
  type ConsoleMessage,
  type Page,
} from "@playwright/test";
import { sessionApi } from "./ports";

// The production Content-Security-Policy, in a real browser. `vite preview` sends the headers of
// infra/deploy/security-headers.caddy (SECURITY_HEADERS=1 in playwright.session.config.ts), the same
// file Caddy serves, against the real API. A policy nobody runs would only look strict.

// The fixture slide is a whole document with its own <meta> CSP; sandboxDocument nests it in <body>, where
// browsers ignore a meta policy and say so. That is not a violation (it predates the header policy, and the
// frame's header CSP governs the slide), so it is the one CSP message this check does not count.
const IGNORED_META =
  /delivered via a <meta> element outside the document's <head>/;
const violation = (message: ConsoleMessage) =>
  !IGNORED_META.test(message.text()) &&
  /Content Security Policy|Refused to (execute|load|connect|apply|frame|create)/i.test(
    message.text(),
  );

function watch(page: Page) {
  const seen: string[] = [];
  page.on("console", (message) => {
    if (violation(message)) seen.push(message.text());
  });
  page.on("pageerror", (error) => {
    if (violation({ text: () => error.message } as ConsoleMessage))
      seen.push(error.message);
  });
  return seen;
}

async function apiTokens(request: APIRequestContext, email: string) {
  const code = await request.post(`${sessionApi}/v1/auth/code`, {
    data: { email, age_confirmed: true },
  });
  expect(code.status()).toBe(202);
  const mail = await (
    await request.get(
      `${sessionApi}/test/mail?email=${encodeURIComponent(email)}`,
    )
  ).json();
  const verified = await request.post(`${sessionApi}/v1/auth/verify`, {
    data: { email, code: mail.code, age_confirmed: true },
  });
  expect(verified.status()).toBe(200);
  return (await verified.json()) as { access_token: string };
}

async function signInInBrowser(
  page: Page,
  email: string,
  request: APIRequestContext,
) {
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

test("the policy is active: an injected inline script is blocked", async ({
  page,
}) => {
  const seen = watch(page);
  const response = await page.goto("/sign-in");
  expect(response!.headers()["content-security-policy"]).toContain(
    "script-src 'self';",
  );
  const ran = await page.evaluate(() => {
    const script = document.createElement("script");
    script.textContent = "window.__injected = true";
    document.head.append(script);
    return (window as unknown as { __injected?: boolean }).__injected === true;
  });
  expect(ran).toBe(false);
  await expect.poll(() => seen.length).toBeGreaterThan(0);
});

test("sign-in page loads with every security header and no violation", async ({
  page,
}) => {
  const seen = watch(page);
  const response = await page.goto("/sign-in");
  await expect(page.getByRole("button", { name: "Get started" })).toBeVisible();
  const headers = response!.headers();
  expect(headers["strict-transport-security"]).toBe(
    "max-age=31536000; includeSubDomains",
  );
  expect(headers["x-content-type-options"]).toBe("nosniff");
  expect(headers["referrer-policy"]).toBe("strict-origin-when-cross-origin");
  expect(headers["permissions-policy"]).toContain("microphone=(self)");
  const csp = headers["content-security-policy"]!;
  for (const part of [
    "default-src 'self'",
    "frame-ancestors 'none'",
    "object-src 'none'",
    "base-uri 'self'",
  ])
    expect(csp).toContain(part);
  expect(csp).not.toContain("unsafe-eval");
  expect(
    csp.split(";").find((d) => d.trim().startsWith("script-src")),
  ).not.toContain("unsafe-inline");
  // Open the SSO page too (Clerk is not configured here, so it shows its own error; no violation).
  await page.goto("/sso-bridge?state=x&strategy=oauth_google&mode=web");
  expect(seen).toEqual([]);
});

test("a lesson plays under the CSP: slide scripts run in the sandbox frame, zero violations", async ({
  page,
  request,
}) => {
  const email = `csp-${crypto.randomUUID()}@example.test`;
  const { access_token } = await apiTokens(request, email);
  const seeded = await request.post(`${sessionApi}/test/player/fixture`, {
    headers: { Authorization: `Bearer ${access_token}` },
  });
  expect(seeded.status()).toBe(201);
  const { lesson_id } = await seeded.json();

  const seen = watch(page);
  const frameResponses: Record<string, string> = {};
  page.on("response", (response) => {
    if (new URL(response.url()).pathname === "/lesson-frame.html")
      frameResponses.csp = response.headers()["content-security-policy"] ?? "";
  });
  await signInInBrowser(page, email, request);
  await page.goto(`/lesson/${lesson_id}`);
  await expect(page.getByText("Slide 1 of 3")).toBeVisible();

  const frame = page.locator('iframe[title="Slide 1"]');
  await expect(frame).toHaveAttribute("sandbox", "allow-scripts");
  await expect(frame).toHaveAttribute("src", /^\/lesson-frame\.html\?v=/);
  const slide = page.frameLocator('iframe[title="Slide 1"]');
  await expect(slide.getByRole("heading", { name: "Look up" })).toBeVisible();
  // The slide's inline script ran and answered the player's command: it set data-motion on <html>.
  await expect(slide.locator("html")).toHaveAttribute(
    "data-motion",
    /^(full|reduce)$/,
  );
  // And the slide still cannot reach the app: opaque origin, no storage.
  const storageDenied = await slide.locator("body").evaluate(() => {
    try {
      void parent.localStorage.length;
      return false;
    } catch {
      return true;
    }
  });
  expect(storageDenied).toBe(true);

  expect(frameResponses.csp).toContain("default-src 'none'");
  expect(frameResponses.csp).toContain("frame-ancestors 'self'");
  expect(frameResponses.csp).not.toContain("unsafe-eval");
  expect(seen).toEqual([]);
});

test("with the network gone the next slide still loads: the frame page comes from the HTTP cache", async ({
  page,
  context,
  request,
}) => {
  // No page.route here on purpose: Playwright disables the HTTP cache while a route is active, and the
  // sandboxed slide host is never seen by the service worker (opaque origin), so the frame page must come from cache.
  const email = `csp-offline-${crypto.randomUUID()}@example.test`;
  const { access_token } = await apiTokens(request, email);
  const seeded = await request.post(`${sessionApi}/test/player/fixture`, {
    headers: { Authorization: `Bearer ${access_token}` },
  });
  const { lesson_id } = await seeded.json();
  await signInInBrowser(page, email, request);
  await page.goto(`/lesson/${lesson_id}`);
  await expect(page.getByText("Available offline")).toBeVisible();
  await expect(
    page.frameLocator('iframe[title="Slide 1"]').locator(".star"),
  ).toBeVisible();
  await page.evaluate(async () => {
    await navigator.serviceWorker.ready;
  });
  await context.setOffline(true);
  // Moving to the next slide reloads the sandbox frame, which now has no network: only the HTTP cache has it.
  await page.getByRole("button", { name: "Next", exact: true }).click();
  await expect(
    page.frameLocator('iframe[title="Slide 2"]').locator(".star"),
  ).toBeVisible();
});
