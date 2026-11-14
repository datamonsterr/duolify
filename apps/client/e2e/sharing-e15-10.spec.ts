import type {
  APIRequestContext,
  Browser,
  BrowserContext,
  Page,
} from "@playwright/test";
import { apiBase, webBase } from "./servers";
import { test, expect } from "./accounts-fixture";

type Tokens = { access_token: string };
type Who = { headers: { Authorization: string }; tokens: Tokens };

async function account(request: APIRequestContext): Promise<Who> {
  const email = `moderation-${crypto.randomUUID()}@example.test`;
  await request.post(`${apiBase}/v1/auth/code`, {
    data: { email, age_confirmed: true },
  });
  const { code } = await (
    await request.get(`${apiBase}/test/mail?email=${email}`)
  ).json();
  const verified = await request.post(`${apiBase}/v1/auth/verify`, {
    data: { email, code, age_confirmed: true },
  });
  expect(verified.status()).toBe(200);
  const tokens = (await verified.json()) as Tokens;
  return {
    headers: { Authorization: `Bearer ${tokens.access_token}` },
    tokens,
  };
}

async function moderator(request: APIRequestContext): Promise<Who> {
  const who = await account(request);
  const grant = await request.post(`${apiBase}/test/moderation/grant`, {
    headers: who.headers,
  });
  expect(grant.status()).toBe(204);
  return who;
}

async function seeded(
  request: APIRequestContext,
  author: Who,
  title: string,
  description: string,
) {
  const res = await request.post(`${apiBase}/test/marketplace/course`, {
    headers: author.headers,
    data: { title, description },
  });
  expect(res.status()).toBe(201);
  const course = (await res.json()).course_id as string;
  const draft = await request.post(
    `${apiBase}/v1/courses/${course}/publication-drafts`,
    { headers: author.headers, data: {} },
  );
  return { course, verdict: (await draft.json()).verdict as string };
}

const list = (request: APIRequestContext, author: Who, course: string) =>
  request.put(`${apiBase}/v1/courses/${course}/marketplace-listing`, {
    headers: author.headers,
    data: { category: "tech", language: "en", tags: [] },
  });

async function inCatalogue(request: APIRequestContext, title: string) {
  const res = await request.get(
    `${apiBase}/v1/marketplace/courses?q=${encodeURIComponent(title)}&sort=new`,
  );
  const body = await res.json();
  return (body.items as { title: string }[]).some((c) => c.title === title);
}

/** A browser context signed in as `who`, forwarding /v1 calls to the real API. */
async function pageAs(
  browser: Browser,
  request: APIRequestContext,
  who: Who,
  mobile: boolean,
): Promise<{ page: Page; context: BrowserContext }> {
  const context = await browser.newContext({
    baseURL: webBase,
    viewport: mobile
      ? { width: 390, height: 844 }
      : { width: 1280, height: 900 },
    hasTouch: mobile,
  });
  await context.addInitScript(() => {
    Object.defineProperty(window, "__TAURI_INTERNALS__", {
      configurable: true,
      value: {
        invoke: async (command: string, args?: { value?: string | null }) => {
          const key = "duolify.native-store-fixture";
          if (command === "read_credentials")
            return sessionStorage.getItem(key);
          if (command === "write_credentials") {
            if (args?.value) sessionStorage.setItem(key, args.value);
            else sessionStorage.removeItem(key);
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
  await context.addInitScript((value) => {
    sessionStorage.setItem(
      "duolify.native-store-fixture",
      JSON.stringify(value),
    );
  }, who.tokens);
  const page = await context.newPage();
  await page.route("**/v1/**", async (route) => {
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
  });
  return { page, context };
}

const noOverflow = async (page: Page) =>
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth - window.innerWidth,
    ),
  ).toBeLessThanOrEqual(0);

async function openRow(page: Page, name: string, mobile: boolean) {
  await page.goto("/moderation");
  await expect(page.getByRole("heading", { name: "Moderation" })).toBeVisible();
  await page.getByRole("button", { name }).click();
  if (mobile) {
    await expect(
      page.getByRole("button", { name: "Back to queue" }),
    ).toBeVisible();
  }
}

for (const mobile of [false, true]) {
  const label = mobile ? "mobile" : "desktop";

  test(`a moderator resolves a borderline publication before it is visible (${label})`, async ({
    browser,
    request,
  }) => {
    const author = await account(request);
    const title = `Chemistry safety ${label}`;
    const { course, verdict } = await seeded(
      request,
      author,
      title,
      "How to store cleaners safely e15-borderline",
    );
    expect(verdict).toBe("needs_review");
    expect((await list(request, author, course)).status()).toBe(409);
    expect(await inCatalogue(request, title)).toBe(false);

    const mod = await moderator(request);
    const { page, context } = await pageAs(browser, request, mod, mobile);
    await openRow(page, `Course · ${title}`, mobile);
    const detail = page.getByRole("region", { name: "Report detail" });
    await expect(detail.getByText("Screening: needs review")).toBeVisible();
    await detail.getByRole("button", { name: "Approve publication" }).click();
    const dialog = page.getByRole("dialog", { name: "Approve publication" });
    await expect(dialog).toBeVisible();
    // A reason is required before any action can be confirmed.
    const confirm = dialog.getByRole("button", { name: "Confirm action" });
    await expect(confirm).toBeDisabled();
    await dialog
      .getByLabel("Reason sent to owner")
      .fill("Educational safety content with no step-by-step hazards.");
    await dialog.getByRole("button", { name: "Confirm action" }).click();
    await expect(page.getByRole("status")).toContainText("Action recorded");
    await expect(
      page.getByRole("button", { name: `Course · ${title}` }),
    ).toBeHidden();

    expect((await list(request, author, course)).status()).toBe(200);
    expect(await inCatalogue(request, title)).toBe(true);
    await noOverflow(page);
    await context.close();
  });

  test(`a severe takedown blocks existing copies and a normal delist does not (${label})`, async ({
    browser,
    request,
  }) => {
    const author = await account(request);
    const title = `Unsafe cleaners ${label}`;
    const { course } = await seeded(request, author, title, "Short lessons");
    expect((await list(request, author, course)).status()).toBe(200);
    const learner = await account(request);
    const enrolled = await request.post(
      `${apiBase}/v1/marketplace/courses/${course}/enroll`,
      { headers: learner.headers, data: {} },
    );
    expect(enrolled.status()).toBe(201);
    const copy = (await enrolled.json()).course_id as string;
    for (const reason of ["unsafe", "unsafe", "spam"]) {
      const res = await request.post(`${apiBase}/v1/marketplace/reports`, {
        headers: (await account(request)).headers,
        data: { target_type: "course", target_id: course, reason },
      });
      expect(res.status()).toBe(202);
    }
    expect(await inCatalogue(request, title)).toBe(false);

    const mod = await moderator(request);
    const { page, context } = await pageAs(browser, request, mod, mobile);
    await openRow(page, `Course · ${title}`, mobile);
    const detail = page.getByRole("region", { name: "Report detail" });
    await expect(detail.getByText("3 reporters")).toBeVisible();
    await detail.getByRole("button", { name: "Take down" }).click();
    const dialog = page.getByRole("dialog", { name: "Take down course" });
    await dialog.getByLabel("Reason code").selectOption("unsafe");
    const block = dialog.getByLabel("Also block existing copies");
    await expect(block).toBeChecked();
    await expect(block).toBeDisabled();
    await dialog
      .getByLabel("Reason sent to owner")
      .fill("Lesson 3 gives instructions that could cause harm.");
    await dialog.getByRole("button", { name: "Confirm action" }).click();
    await expect(page.getByRole("status")).toContainText("Action recorded");

    expect(await inCatalogue(request, title)).toBe(false);
    const newcomer = await account(request);
    const refused = await request.post(
      `${apiBase}/v1/marketplace/courses/${course}/enroll`,
      { headers: newcomer.headers, data: {} },
    );
    expect(refused.status()).toBe(404);
    const mine = await request.get(`${apiBase}/v1/courses/${copy}`, {
      headers: learner.headers,
    });
    expect((await mine.json()).moderation_blocked).toBe(true);

    // The learner with a blocked copy sees a notice instead of the lessons.
    const learnerPage = await pageAs(browser, request, learner, mobile);
    await learnerPage.page.goto(`/moderation/blocked/${copy}`);
    await expect(
      learnerPage.page.getByRole("heading", {
        name: "This course was removed",
      }),
    ).toBeVisible();
    await noOverflow(learnerPage.page);
    await learnerPage.context.close();

    // A normal delist keeps pinned access.
    const other = await account(request);
    const normalTitle = `Spammy list ${label}`;
    const normal = await seeded(request, other, normalTitle, "Short lessons");
    expect((await list(request, other, normal.course)).status()).toBe(200);
    const kept = await request.post(
      `${apiBase}/v1/marketplace/courses/${normal.course}/enroll`,
      { headers: learner.headers, data: {} },
    );
    const keptCopy = (await kept.json()).course_id as string;
    for (let i = 0; i < 3; i++) {
      await request.post(`${apiBase}/v1/marketplace/reports`, {
        headers: (await account(request)).headers,
        data: {
          target_type: "course",
          target_id: normal.course,
          reason: "spam",
        },
      });
    }
    await page.reload();
    await page.getByRole("button", { name: `Course · ${normalTitle}` }).click();
    await page
      .getByRole("region", { name: "Report detail" })
      .getByRole("button", { name: "Delist" })
      .click();
    const d2 = page.getByRole("dialog", { name: "Delist course" });
    await d2.getByLabel("Reason code").selectOption("spam");
    await expect(d2.getByLabel("Also block existing copies")).not.toBeChecked();
    await d2.getByLabel("Reason sent to owner").fill("Promotional content.");
    await d2.getByRole("button", { name: "Confirm action" }).click();
    await expect(page.getByRole("status")).toContainText("Action recorded");
    const still = await request.get(`${apiBase}/v1/courses/${keptCopy}`, {
      headers: learner.headers,
    });
    expect((await still.json()).moderation_blocked).toBeFalsy();
    await context.close();
  });

  test(`an author sees the reason and appeals once, decided by another moderator (${label})`, async ({
    browser,
    request,
  }) => {
    const author = await account(request);
    const title = `Appealed course ${label}`;
    const { course } = await seeded(request, author, title, "Short lessons");
    expect((await list(request, author, course)).status()).toBe(200);
    for (const reason of ["misleading", "misleading", "other"]) {
      await request.post(`${apiBase}/v1/marketplace/reports`, {
        headers: (await account(request)).headers,
        data: { target_type: "course", target_id: course, reason },
      });
    }
    const first = await moderator(request);
    const second = await moderator(request);
    const queue = await (
      await request.get(`${apiBase}/v1/moderation/queue`, {
        headers: first.headers,
      })
    ).json();
    const item = (queue.items as { id: string; target_id: string }[]).find(
      (i) => i.target_id === course,
    )!;
    const acted = await request.post(
      `${apiBase}/v1/moderation/items/${item.id}/actions`,
      {
        headers: first.headers,
        data: {
          action: "delist",
          reason_code: "misleading",
          reason: "The title promises more than the lessons deliver.",
        },
      },
    );
    expect(acted.status()).toBe(201);
    const actionId = (await acted.json()).id as string;

    // A user without notices sees no entry point in navigation.
    const cleanUser = await account(request);
    const { page: cleanPage, context: cleanCtx } = await pageAs(
      browser,
      request,
      cleanUser,
      mobile,
    );
    await cleanPage.goto("/");
    await expect(
      cleanPage.getByRole("link", { name: /moderation notices/i }),
    ).toHaveCount(0);
    if (mobile) {
      await cleanPage.goto("/more");
      await expect(
        cleanPage.getByRole("link", { name: /moderation notices/i }),
      ).toHaveCount(0);
    }
    await cleanCtx.close();

    const { page, context } = await pageAs(browser, request, author, mobile);

    // Affected course displays a notice banner linking to /moderation/notices.
    await page.goto(`/course/${course}`);
    const banner = page
      .getByRole("status")
      .filter({ hasText: /moderation notice/i });
    await expect(banner).toBeVisible();
    await expect(
      banner.getByRole("link", { name: /moderation notices|notices/i }),
    ).toBeVisible();

    // Navigate to /moderation/notices from the navigation entry point.
    if (mobile) {
      await page
        .getByRole("navigation", { name: "Main" })
        .getByRole("link", { name: "More" })
        .click();
      const navLink = page
        .getByRole("navigation", { name: "More destinations" })
        .getByRole("link", { name: /moderation notices/i });
      await expect(navLink).toBeVisible();
      await expect(navLink).toHaveAttribute("aria-label", /1/);
      await navLink.click();
    } else {
      const navLink = page
        .getByRole("navigation", { name: "Main" })
        .getByRole("link", { name: /moderation notices/i });
      await expect(navLink).toBeVisible();
      await expect(navLink).toHaveAttribute("aria-label", /1/);
      await navLink.click();
    }

    await expect(
      page.getByRole("heading", { name: "Moderation notices" }),
    ).toBeVisible();
    const notice = page.getByRole("article", {
      name: "Notice: course delisted",
    });
    await expect(notice).toContainText(
      "The title promises more than the lessons deliver.",
    );
    await notice.getByRole("button", { name: "Appeal decision" }).click();
    const dialog = page.getByRole("dialog", { name: "Appeal this decision" });
    await expect(
      dialog.getByRole("button", { name: "Send appeal" }),
    ).toBeDisabled();
    await dialog
      .getByLabel("Why should this be reviewed?")
      .fill("The lessons cover every topic named in the title.");
    await dialog.getByRole("button", { name: "Send appeal" }).click();
    await expect(page.getByRole("status")).toContainText("Appeal sent");
    await expect(notice.getByText("Appeal under review")).toBeVisible();
    await expect(
      notice.getByRole("button", { name: "Appeal decision" }),
    ).toBeHidden();

    // A second appeal is refused by the API as well.
    const again = await request.post(
      `${apiBase}/v1/moderation/notices/${actionId}/appeal`,
      { headers: author.headers, data: { text: "Again, please." } },
    );
    expect(again.status()).toBe(409);

    const open = await (
      await request.get(`${apiBase}/v1/moderation/queue`, {
        headers: first.headers,
      })
    ).json();
    const appeal = (open.appeals as { id: string }[])[0];
    const same = await request.post(
      `${apiBase}/v1/moderation/appeals/${appeal.id}/decision`,
      {
        headers: first.headers,
        data: { decision: "uphold", reason: "Same moderator." },
      },
    );
    expect(same.status()).toBe(403);
    const decided = await request.post(
      `${apiBase}/v1/moderation/appeals/${appeal.id}/decision`,
      {
        headers: second.headers,
        data: {
          decision: "overturn",
          reason: "The lessons do match the title.",
        },
      },
    );
    expect(decided.status()).toBe(204);
    await page.reload();
    await expect(
      page
        .getByRole("article", { name: "Notice: course delisted" })
        .getByText("Appeal overturned"),
    ).toBeVisible();
    expect(await inCatalogue(request, title)).toBe(true);
    await noOverflow(page);
    await context.close();
  });
}

test("moderator queue states: error retry, empty, overdue, long content, keyboard, dark and reduced motion", async ({
  browser,
  request,
}) => {
  const mod = await moderator(request);
  const { page, context } = await pageAs(browser, request, mod, true);
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
  let calls = 0;
  const long =
    "A very long course title that must wrap without overflow 界 ".repeat(4);
  await page.route("**/v1/moderation/queue", async (route) => {
    calls += 1;
    if (calls === 1) return route.abort();
    if (calls === 2)
      return route.fulfill({
        json: {
          items: [],
          appeals: [],
          open_count: 0,
          overdue_count: 0,
          screening_count: 0,
        },
      });
    return route.fulfill({
      json: {
        items: [
          {
            id: "00000000-0000-4000-8000-000000000001",
            target_type: "course",
            target_id: "00000000-0000-4000-8000-000000000002",
            course_id: "00000000-0000-4000-8000-000000000002",
            title: long,
            severity: 100,
            reasons: ["unsafe"],
            report_count: 4,
            opened_at: "2026-11-01T00:00:00Z",
            due_at: "2026-11-03T00:00:00Z",
            overdue: true,
            hidden: true,
            version: 2,
            audit: [],
          },
        ],
        appeals: [],
        open_count: 1,
        overdue_count: 1,
        screening_count: 0,
      },
    });
  });
  await page.goto("/moderation");
  await expect(page.getByRole("alert")).toContainText("Couldn't reach Duolify");
  await page.getByRole("button", { name: "Retry", exact: true }).click();
  await expect(page.getByText("Nothing is waiting for review")).toBeVisible();
  await page.getByRole("button", { name: "Refresh queue" }).click();
  const row = page.getByRole("button", { name: /^Course · A very long/ });
  await expect(row).toBeVisible();
  await expect(page.getByText("1 over 48 h")).toBeVisible();
  await row.click();
  await expect(page.getByText("Past 48 h target")).toBeVisible();
  const take = page.getByRole("button", { name: "Take down" });
  await take.click();
  const dialog = page.getByRole("dialog", { name: "Take down course" });
  await expect(dialog.getByLabel("Reason code")).toBeFocused();
  expect(
    await dialog.evaluate((el) => getComputedStyle(el).animationName),
  ).toBe("none");
  await page.keyboard.press("Escape");
  await expect(dialog).toBeHidden();
  await expect(take).toBeFocused();
  expect(
    await page.evaluate(() => getComputedStyle(document.body).backgroundColor),
  ).not.toBe("rgb(255, 255, 255)");
  await noOverflow(page);
  await context.close();
});

test("a person without the moderator role sees nothing", async ({
  browser,
  request,
}) => {
  const plain = await account(request);
  const res = await request.get(`${apiBase}/v1/moderation/queue`, {
    headers: plain.headers,
  });
  expect(res.status()).toBe(404);
  const { page, context } = await pageAs(browser, request, plain, false);
  await page.goto("/moderation");
  await expect(
    page.getByRole("heading", { name: "This page isn't available" }),
  ).toBeVisible();
  await context.close();
});
