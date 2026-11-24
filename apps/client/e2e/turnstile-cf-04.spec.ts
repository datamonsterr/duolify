import {
  expect,
  test,
  type APIRequestContext,
  type Page,
} from "@playwright/test";
import { apiBase } from "./servers";

const passSiteKey = "1x00000000000000000000AA";
const failSiteKey = "2x00000000000000000000AB";
const proofHeader = "X-Turnstile-Token";
const screenshotDir = "e2e/__screenshots__/cf-04";

async function fixture(
  page: Page,
  request: APIRequestContext,
  mode: "pass" | "fail" | "disabled",
  siteKey = passSiteKey,
) {
  let apiMode = mode;
  const requests: { path: string; proof?: string }[] = [];
  await page.route(
    "https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit",
    (route) =>
      route.fulfill({
        contentType: "application/javascript",
        body: `
      const widgets = new Map();
      window.turnstile = {
        render(container, options) {
          const id = crypto.randomUUID();
          const widget = document.createElement('div');
          widget.className = 'turnstile-test-widget';
          widget.style.cssText = 'display:flex;align-items:center;gap:10px;padding:12px;border:1px solid #7a7a7a;border-radius:8px;max-width:100%;width:300px;min-height:65px;background:white;color:#2b2b2b';
          if (options.appearance === 'interaction-only') widget.style.display = 'none';
          else widget.innerHTML = '<span aria-hidden="true" style="color:#1f7a0d">' + (options.action === 'share_preview' ? '…' : '✓') + '</span><span>' + (options.action === 'share_preview' ? 'Verifying' : 'Verification complete') + '</span><span style="margin-inline-start:auto;font-size:12px">Cloudflare</span>';
          container.append(widget);
          const complete = () => {
            if (options.sitekey === ${JSON.stringify(failSiteKey)}) options['error-callback']('110200');
            else if (options.sitekey === ${JSON.stringify(passSiteKey)}) options.callback(options.action + ':' + crypto.randomUUID());
          };
          widgets.set(id, { widget, complete });
          if (options.action !== 'share_preview') queueMicrotask(complete);
          return id;
        },
        remove(id) { widgets.get(id)?.widget.remove(); widgets.delete(id); },
      };
      window.addEventListener('test-turnstile-complete', () => {
        for (const { complete } of widgets.values()) complete();
      });
    `,
      }),
  );
  await page.route(/\/v1\/(auth\/|public\/shares\/)/, async (route) => {
    const original = route.request();
    const { pathname, search } = new URL(original.url());
    const headers = original.headers();
    delete headers.origin;
    const body = original.postDataJSON() as { turnstile_token?: string } | null;
    if (
      original.method() === "POST" ||
      pathname.startsWith("/v1/public/shares/")
    ) {
      requests.push({
        path: pathname,
        proof: body?.turnstile_token ?? headers[proofHeader.toLowerCase()],
      });
    }
    const prefix =
      apiMode === "disabled" || pathname === "/v1/auth/sso/config"
        ? ""
        : `/test/turnstile/${apiMode}`;
    const response = await request.fetch(
      `${apiBase}${prefix}${pathname}${search}`,
      {
        method: original.method(),
        headers,
        data: original.postData() ?? undefined,
      },
    );
    if (
      pathname === "/v1/auth/turnstile/config" &&
      siteKey === failSiteKey &&
      response.ok()
    ) {
      await route.fulfill({
        response,
        json: { ...(await response.json()), site_key: siteKey },
      });
    } else await route.fulfill({ response });
  });
  return {
    requests,
    pass: () => {
      apiMode = "pass";
    },
  };
}

async function emailStep(page: Page, email: string, guided = true) {
  if (!guided)
    await page.addInitScript(() =>
      localStorage.setItem("duolify.guided.signin", "off"),
    );
  await page.goto("/sign-in");
  await page.getByRole("button", { name: "Get started" }).click();
  await page.getByLabel("Email address").fill(email);
  if (guided) await page.getByLabel("Email address").press("Enter");
  await page.getByLabel("I am 16 or older").check();
}

async function mailbox(
  request: APIRequestContext,
  email: string,
): Promise<string> {
  const response = await request.get(`${apiBase}/test/mail`, {
    params: { email },
  });
  return (await response.json()).code;
}

async function seedPreview(request: APIRequestContext) {
  const email = `cf04-owner-${crypto.randomUUID()}@example.test`;
  const data = { email, age_confirmed: true };
  expect(
    (await request.post(`${apiBase}/v1/auth/code`, { data })).status(),
  ).toBe(202);
  const signed = await request.post(`${apiBase}/v1/auth/verify`, {
    data: { ...data, code: await mailbox(request, email) },
  });
  expect(signed.status()).toBe(200);
  const headers = {
    Authorization: `Bearer ${(await signed.json()).access_token}`,
  };
  const seeded = await request.post(`${apiBase}/test/sharing/course`, {
    headers,
    data: { variant: "rich" },
  });
  expect(seeded.status()).toBe(201);
  const { course_id } = await seeded.json();
  const course = `${apiBase}/v1/courses/${course_id}`;
  expect(
    (
      await request.post(`${course}/publication-drafts`, { headers, data: {} })
    ).status(),
  ).toBe(201);
  const link = await request.post(`${course}/share-link`, {
    headers,
    data: {},
  });
  expect(link.status()).toBe(201);
  return (await link.json()).token as string;
}

for (const [label, width, height] of [
  ["desktop", 1280, 800],
  ["mobile", 390, 844],
] as const) {
  test(`requests email, signs up and views a share with separate proofs on ${label}`, async ({
    page,
    request,
  }) => {
    await page.setViewportSize({ width, height });
    const { requests } = await fixture(page, request, "pass");
    const email = `learner-${label}@example.test`;
    await emailStep(page, email);
    await expect(
      page.getByRole("group", { name: "Security check" }),
    ).toBeVisible();
    await expect(page.getByText("Verification complete")).toBeVisible();
    expect(await mailbox(request, email)).toBe("");
    await page.screenshot({
      animations: "disabled",
      path: `${screenshotDir}/email-code-${label}.png`,
    });
    await page.getByRole("button", { name: "Send code" }).click();
    await expect(page.getByLabel("One-time code")).toBeVisible();
    await expect(page.getByText("Verification complete")).toBeVisible();
    const code = await mailbox(request, email);
    expect(code).toMatch(/^\d{6}$/);
    await page.screenshot({
      animations: "disabled",
      path: `${screenshotDir}/sign-up-${label}.png`,
    });
    await page.getByLabel("One-time code").fill(code);
    const verification = page.waitForResponse((response) =>
      response.url().endsWith("/v1/auth/verify"),
    );
    await page.getByRole("button", { name: "Sign in", exact: true }).click();
    expect((await verification).status()).toBe(200);
    const [codeRequest, signupRequest] = requests;
    expect(codeRequest).toMatchObject({
      path: "/v1/auth/code",
      proof: expect.stringMatching(/^email_code:/),
    });
    expect(signupRequest).toMatchObject({
      path: "/v1/auth/verify",
      proof: expect.stringMatching(/^sign_up:/),
    });

    const token = await seedPreview(request);
    const sharePath = `/v1/public/shares/${token}`;
    await page.goto(`/c/${token}`);
    await expect(
      page.getByRole("group", { name: "Security check" }),
    ).toBeVisible();
    await expect(page.getByText("Verifying", { exact: true })).toBeVisible();
    expect(requests.some(({ path }) => path === sharePath)).toBe(false);
    await page.screenshot({
      animations: "disabled",
      path: `${screenshotDir}/share-preview-${label}.png`,
    });
    await page.evaluate(() =>
      window.dispatchEvent(new Event("test-turnstile-complete")),
    );
    await expect(
      page.getByRole("heading", { name: "Astronomy for curious beginners" }),
    ).toBeVisible();
    expect(requests.find(({ path }) => path === sharePath)?.proof).toMatch(
      /^share_preview:/,
    );
  });
}

test("the fallback form verifies fresh proof for resend and sign-up", async ({
  page,
  request,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  const { requests } = await fixture(page, request, "pass");
  const email = `cf04-form-${crypto.randomUUID()}@example.test`;
  await emailStep(page, email, false);
  await page.getByRole("button", { name: "Send code" }).click();
  await expect(page.getByLabel("One-time code")).toBeVisible();
  const resent = page.waitForResponse((response) =>
    response.url().endsWith("/v1/auth/code"),
  );
  await page.getByRole("button", { name: "Resend code" }).click();
  expect((await resent).status()).toBe(202);
  await page.getByLabel("One-time code").fill(await mailbox(request, email));
  const verified = page.waitForResponse((response) =>
    response.url().endsWith("/v1/auth/verify"),
  );
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  expect((await verified).status()).toBe(200);
  expect(requests.slice(0, 3).map(({ proof }) => proof)).toEqual([
    expect.stringMatching(/^email_code:/),
    expect.stringMatching(/^email_code:/),
    expect.stringMatching(/^sign_up:/),
  ]);
  expect(requests[1]?.proof).not.toBe(requests[0]?.proof);
});

test("a rejected share stays hidden and retry sends a fresh header", async ({
  page,
  request,
}) => {
  const { requests, pass } = await fixture(page, request, "fail");
  const token = await seedPreview(request);
  await page.goto(`/c/${token}`);
  const preview = page.getByRole("heading", {
    name: "Astronomy for curious beginners",
  });
  const complete = async () => {
    await expect(page.getByText("Verifying", { exact: true })).toBeVisible();
    await page.evaluate(() =>
      window.dispatchEvent(new Event("test-turnstile-complete")),
    );
  };
  await complete();
  const retry = page.getByRole("button", { name: "Try security check again" });
  await expect(retry).toBeFocused();
  await expect(preview).toHaveCount(0);
  pass();
  await retry.press("Enter");
  await complete();
  await expect(preview).toBeVisible();
  expect(requests.map(({ proof }) => proof)).toEqual([
    expect.stringMatching(/^share_preview:/),
    expect.stringMatching(/^share_preview:/),
  ]);
  expect(requests[1]?.proof).not.toBe(requests[0]?.proof);
});

test("a failed Siteverify secret blocks delivery and keyboard retry uses fresh proof", async ({
  page,
  request,
}) => {
  const { requests, pass } = await fixture(page, request, "fail");
  const email = `cf04-fail-${crypto.randomUUID()}@example.test`;
  await emailStep(page, email);
  await page.getByRole("button", { name: "Send code" }).click();
  const retry = page.getByRole("button", { name: "Try security check again" });
  await expect(retry).toBeFocused();
  await expect(page.getByRole("alert")).toContainText("Security check failed");
  expect(await mailbox(request, email)).toBe("");
  pass();
  await retry.press("Enter");
  await page.getByRole("button", { name: "Send code" }).click();
  await expect(page.getByLabel("One-time code")).toBeVisible();
  expect(requests[0]?.proof).toMatch(/^email_code:/);
  expect(requests[1]?.proof).toMatch(/^email_code:/);
  expect(requests[0]?.proof).not.toBe(requests[1]?.proof);
});

test("the official failing widget key offers a retry without submitting", async ({
  page,
  request,
}) => {
  const { requests } = await fixture(page, request, "pass", failSiteKey);
  await emailStep(page, `cf04-widget-${crypto.randomUUID()}@example.test`);
  await expect(
    page.getByRole("button", { name: "Try security check again" }),
  ).toBeVisible();
  await expect(page.getByRole("button", { name: "Send code" })).toBeDisabled();
  expect(requests).toEqual([]);
});

test("disabled Turnstile sends email without loading the script", async ({
  page,
  request,
}) => {
  const scripts: string[] = [];
  page.on("request", (req) => {
    if (req.url().startsWith("https://challenges.cloudflare.com/"))
      scripts.push(req.url());
  });
  const { requests } = await fixture(page, request, "disabled");
  await emailStep(page, `cf04-off-${crypto.randomUUID()}@example.test`);
  await page.getByRole("button", { name: "Send code" }).click();
  await expect(page.getByLabel("One-time code")).toBeVisible();
  expect(requests[0]?.proof).toBeUndefined();
  expect(scripts).toEqual([]);
});
