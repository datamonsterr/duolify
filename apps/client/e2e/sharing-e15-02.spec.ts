import type { APIRequestContext, Browser, Page } from "@playwright/test";
import { apiBase, webBase } from "./servers";
import { test, expect } from "./accounts-fixture";

type Headers = { Authorization: string };
const PRIVATE_MARKER = "E15-PRIVATE-MARKER";

async function tokenOf(page: Page): Promise<Headers> {
  await page.goto("/profile");
  const tokens = await page.evaluate(() =>
    JSON.parse(sessionStorage.getItem("duolify.native-store-fixture")!),
  );
  return { Authorization: `Bearer ${tokens.access_token}` };
}

async function seed(
  request: APIRequestContext,
  headers: Headers,
  variant: "rich" | "pii",
) {
  const response = await request.post(`${apiBase}/test/sharing/course`, {
    headers,
    data: { variant },
  });
  expect(response.status()).toBe(201);
  return (await response.json()).course_id as string;
}

// Course, feature and XP calls from the owner page go to the E2E API server.
async function proxyCourses(page: Page, request: APIRequestContext) {
  await page.route(
    /\/v1\/(courses|features|gamification)(\/|$|\?)/,
    async (route) => {
      const original = route.request();
      const target = new URL(original.url());
      const headers = original.headers();
      delete headers.origin;
      target.host = new URL(apiBase).host;
      target.protocol = new URL(apiBase).protocol;
      await route.fulfill({
        response: await request.fetch(target.href, {
          method: original.method(),
          headers,
          data: original.postData() ?? undefined,
        }),
      });
    },
  );
}

/** An anonymous visitor: a fresh context with no tokens. */
async function visitor(
  browser: Browser,
  request: APIRequestContext,
  viewport = { width: 1280, height: 800 },
) {
  const context = await browser.newContext({
    baseURL: webBase,
    viewport,
    hasTouch: viewport.width < 768,
  });
  const page = await context.newPage();
  await page.route("**/v1/public/**", async (route) => {
    const original = route.request();
    const target = new URL(original.url());
    target.host = new URL(apiBase).host;
    target.protocol = new URL(apiBase).protocol;
    await route.fulfill({ response: await request.fetch(target.href) });
  });
  return { context, page };
}

async function openShare(page: Page, course: string) {
  await page.goto(`/course/${course}`);
  await page.getByRole("button", { name: "Share course" }).click();
  const dialog = page.getByRole("dialog", { name: /^Share/ });
  await expect(dialog).toBeVisible();
  return dialog;
}

async function publishLink(page: Page, course: string) {
  const dialog = await openShare(page, course);
  await dialog.getByRole("radio", { name: /Anyone with the link/ }).click();
  await expect(dialog.getByTestId("share-link-url")).toContainText("/c/", {
    timeout: 15000,
  });
  return dialog;
}

async function copiedLink(page: Page, dialog: ReturnType<Page["locator"]>) {
  await dialog.getByRole("button", { name: "Copy link" }).click();
  await expect(dialog.getByText("Link copied")).toBeVisible();
  const url = await page.evaluate(() => navigator.clipboard.readText());
  expect(url).toMatch(/\/c\/[A-Za-z0-9_-]{20,}$/);
  return new URL(url).pathname; // /c/<token>
}

test.beforeEach(async ({ context }) => {
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
});

test("owner publishes a link; an anonymous visitor sees the safe preview, noindex, and can launch the app", async ({
  page,
  request,
  browser,
}) => {
  const headers = await tokenOf(page);
  const course = await seed(request, headers, "rich");
  await proxyCourses(page, request);
  const dialog = await publishLink(page, course);
  await expect(dialog.getByTestId("share-link-url")).not.toContainText(
    /[A-Za-z0-9_-]{20,}/,
  ); // the on-screen link is shortened
  await expect(dialog).toContainText("Never shared");
  const path = await copiedLink(page, dialog);

  const { context, page: anon } = await visitor(browser, request);
  const apiResponse = anon.waitForResponse(/\/v1\/public\/shares\//);
  await anon.goto(path);
  expect((await apiResponse).headers()["x-robots-tag"]).toContain("noindex");
  await expect(
    anon.getByRole("heading", { name: "Astronomy for curious beginners" }),
  ).toBeVisible();
  await expect(anon.getByTestId("creator")).toContainText(/^by /);
  await expect(anon.locator('meta[name="robots"]')).toHaveAttribute(
    "content",
    /noindex/,
    { timeout: 5000 },
  );
  // Up to three sample slides; paging keeps the same region.
  await expect(anon.getByText("Preview 1 of 3")).toBeVisible();
  await anon.getByRole("button", { name: "Next slide" }).click();
  await expect(anon.getByText("Preview 2 of 3")).toBeVisible();
  await anon.getByRole("button", { name: "Next slide" }).click();
  await expect(anon.getByRole("button", { name: "Next slide" })).toBeDisabled();
  await expect(anon.getByTestId("lessons-ready")).toContainText(
    /\d+ of \d+ lessons ready/,
  );
  await expect(anon.getByText(/Ready lessons are free/)).toBeVisible();
  await expect(anon.getByTestId("enrollment-count")).toBeVisible();
  // Answer key, explanations and private text never reach the page.
  const html = await anon.content();
  for (const banned of ["Relative angles", "answer_index", PRIVATE_MARKER])
    expect(html).not.toContain(banned);
  await anon.getByRole("button", { name: "See full outline" }).click();
  await expect(
    anon.getByRole("list", { name: "Course outline" }),
  ).toContainText("Phases of the Moon");
  await expect(
    anon.getByRole("link", { name: "Sign in to enroll" }),
  ).toHaveAttribute("href", /\/sign-in\?returnTo=/);
  await expect(
    anon.getByRole("link", { name: "Open in Duolify app" }),
  ).toHaveAttribute("href", /^duolify:\/\/share\/[A-Za-z0-9_-]{20,}$/);
  await context.close();
});

test("revoking kills the link at once; rotating swaps old for new", async ({
  page,
  request,
  browser,
}) => {
  const headers = await tokenOf(page);
  const course = await seed(request, headers, "rich");
  await proxyCourses(page, request);
  const dialog = await publishLink(page, course);
  const first = await copiedLink(page, dialog);
  const { context, page: anon } = await visitor(browser, request);
  await anon.goto(first);
  await expect(
    anon.getByRole("heading", { name: "Astronomy for curious beginners" }),
  ).toBeVisible();

  await dialog.getByRole("button", { name: "New link" }).click();
  await expect(dialog.getByText("Link replaced")).toBeVisible();
  const second = await copiedLink(page, dialog);
  expect(second).not.toBe(first);
  await anon.reload();
  await expect(anon.getByText(/no longer active/)).toBeVisible();
  await anon.goto(second);
  await expect(
    anon.getByRole("heading", { name: "Astronomy for curious beginners" }),
  ).toBeVisible();

  await dialog.getByRole("button", { name: "Revoke link" }).click();
  await expect(dialog.getByText("Link revoked")).toBeVisible();
  await expect(dialog.getByRole("radio", { name: "Private" })).toBeChecked();
  await anon.reload();
  await expect(
    anon.getByText(
      "This course link is no longer active. Ask the creator for a new one.",
    ),
  ).toBeVisible();
  // Revoked, rotated and unknown tokens are indistinguishable.
  for (const gone of [first, second, "/c/" + "A".repeat(27)]) {
    const r = await request.get(
      `${apiBase}/v1/public/shares/${gone.split("/").pop()}`,
    );
    expect(r.status()).toBe(404);
    expect(await r.json()).toEqual({ error: "link_not_found" });
  }
  await context.close();
});

test("a draft that needs review stays private and offers no link", async ({
  page,
  request,
}) => {
  const headers = await tokenOf(page);
  const course = await seed(request, headers, "pii");
  await proxyCourses(page, request);
  const dialog = await openShare(page, course);
  await dialog.getByRole("radio", { name: /Anyone with the link/ }).click();
  await expect(dialog.getByRole("alert")).toContainText(
    /Needs a quick review|can't be published/,
  );
  await expect(dialog.getByTestId("share-link-url")).toHaveCount(0);
  await expect(dialog.getByRole("radio", { name: "Private" })).toBeChecked();
});

test("only the owner can publish; a stranger gets the neutral refusal", async ({
  page,
  request,
}) => {
  const headers = await tokenOf(page);
  const course = await seed(request, headers, "rich");
  const stranger = await request.post(
    `${apiBase}/v1/courses/${course}/share-link`,
  );
  expect(stranger.status()).toBe(401);
  const build = await request.post(
    `${apiBase}/v1/courses/${course}/publication-drafts`,
    { headers, data: {} },
  );
  expect(build.status()).toBe(201);
  const created = await request.post(
    `${apiBase}/v1/courses/${course}/share-link`,
    {
      headers,
      data: {},
    },
  );
  expect(created.status()).toBe(201);
  const body = await created.json();
  expect(body.token).toMatch(/^[A-Za-z0-9_-]{27,}$/);
  expect(body.deep_link).toBe(`duolify://share/${body.token}`);
  const status = await request.get(
    `${apiBase}/v1/courses/${course}/share-link`,
    {
      headers,
    },
  );
  const text = await status.text();
  expect(text).not.toContain(body.token); // the plaintext is shown once
  expect(JSON.parse(text)).toMatchObject({ visibility: "link", active: true });
});

test("mobile preview: sheet-sized layout, touch paging and offline recovery", async ({
  page,
  request,
  browser,
}) => {
  const headers = await tokenOf(page);
  const course = await seed(request, headers, "rich");
  const created = await (
    await request.post(`${apiBase}/v1/courses/${course}/publication-drafts`, {
      headers,
      data: {},
    })
  ).json();
  expect(created.verdict).toBe("pass");
  const link = await (
    await request.post(`${apiBase}/v1/courses/${course}/share-link`, {
      headers,
      data: {},
    })
  ).json();
  const { context, page: anon } = await visitor(browser, request, {
    width: 390,
    height: 800,
  });
  let fail = true;
  await anon.route("**/v1/public/**", async (route) => {
    if (fail) await route.abort("connectionfailed");
    else await route.fallback();
  });
  await anon.goto(`/c/${link.token}`);
  const alert = anon.getByRole("alert");
  await expect(alert).toContainText(/Couldn't reach Duolify/);
  fail = false;
  await alert.getByRole("button", { name: "Retry" }).click();
  await expect(anon.getByText("Preview 1 of 3")).toBeVisible();
  const box = await anon.getByRole("heading", { level: 1 }).boundingBox();
  expect(box!.width).toBeLessThanOrEqual(390);
  expect(
    await anon.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
  await anon.getByRole("button", { name: "Next slide" }).tap();
  await expect(anon.getByText("Preview 2 of 3")).toBeVisible();
  await context.close();
});

test("the public preview is rate limited per IP and never sets an index signal", async ({
  page,
  request,
}) => {
  const headers = await tokenOf(page);
  const course = await seed(request, headers, "rich");
  await request.post(`${apiBase}/v1/courses/${course}/publication-drafts`, {
    headers,
    data: {},
  });
  const link = await (
    await request.post(`${apiBase}/v1/courses/${course}/share-link`, {
      headers,
      data: {},
    })
  ).json();
  const url = `${apiBase}/v1/public/shares/${link.token}`;
  const statuses: number[] = [];
  for (let i = 0; i < 80; i++) {
    const r = await request.get(url);
    expect(r.headers()["x-robots-tag"]).toContain("noindex");
    statuses.push(r.status());
    if (r.status() === 429) {
      expect(r.headers()["retry-after"]).toBeTruthy();
      break;
    }
  }
  expect(statuses).toContain(429);
  expect(statuses[0]).toBe(200);
});
