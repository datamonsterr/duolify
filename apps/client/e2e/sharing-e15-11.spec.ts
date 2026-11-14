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
  const email = `retention-${crypto.randomUUID()}@example.test`;
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

async function shared(
  request: APIRequestContext,
  owner: Who,
  learner: Who,
  title: string,
) {
  const { course, verdict } = await seeded(
    request,
    owner,
    title,
    "Short lessons",
  );
  expect(verdict).toBe("pass");
  expect((await list(request, owner, course)).status()).toBe(200);
  const link = await request.post(
    `${apiBase}/v1/courses/${course}/share-link`,
    {
      headers: owner.headers,
      data: {},
    },
  );
  expect(link.status()).toBeLessThan(300);
  const token = (await link.json()).token as string;
  const enrolled = await request.post(
    `${apiBase}/v1/marketplace/courses/${course}/enroll`,
    { headers: learner.headers, data: {} },
  );
  expect(enrolled.status()).toBe(201);
  return { course, token, copy: (await enrolled.json()).course_id as string };
}

const restore = (request: APIRequestContext, who: Who, rewind: boolean) =>
  request.post(`${apiBase}/test/sharing/restore`, {
    headers: who.headers,
    data: { rewind_blocks: rewind },
  });

for (const mobile of [false, true]) {
  const label = mobile ? "mobile" : "desktop";

  test(`owner unpublishes: link, marketplace and enrollment stop, the copy keeps working (${label})`, async ({
    browser,
    request,
  }) => {
    const owner = await account(request);
    const learner = await account(request);
    const title = `Unpublished tides ${label}`;
    const { course, token, copy } = await shared(
      request,
      owner,
      learner,
      title,
    );
    expect(await inCatalogue(request, title)).toBe(true);

    const { page, context } = await pageAs(browser, request, owner, mobile);
    await page.goto(`/course/${course}`);
    await page.getByTestId("share-course").click();
    const panel = page.getByTestId("share-course-panel");
    await panel.getByRole("button", { name: "Unpublish course" }).click();
    const dialog = page.getByRole("dialog", { name: "Unpublish this course?" });
    await expect(dialog).toContainText("keep their copy");
    // Cancel changes nothing; focus returns to the trigger.
    await dialog.getByRole("button", { name: "Keep published" }).click();
    expect(await inCatalogue(request, title)).toBe(true);
    await panel.getByRole("button", { name: "Unpublish course" }).click();
    await page
      .getByRole("dialog", { name: "Unpublish this course?" })
      .getByRole("button", { name: "Unpublish" })
      .click();
    await expect(panel.getByRole("status").last()).toContainText("Unpublished");
    await noOverflow(page);
    await context.close();

    expect(await inCatalogue(request, title)).toBe(false);
    const preview = await request.get(`${apiBase}/v1/public/shares/${token}`);
    expect(preview.status()).toBe(404);
    const newcomer = await account(request);
    const refused = await request.post(
      `${apiBase}/v1/marketplace/courses/${course}/enroll`,
      { headers: newcomer.headers, data: {} },
    );
    expect(refused.status()).toBe(404);
    const mine = await request.get(`${apiBase}/v1/courses/${copy}`, {
      headers: learner.headers,
    });
    expect(mine.status()).toBe(200);
    expect((await mine.json()).moderation_blocked).toBeFalsy();
  });

  test(`owner deletes their account: the learner copy keeps working as created by a deleted user and survives a restore (${label})`, async ({
    browser,
    request,
  }) => {
    const owner = await account(request);
    const learner = await account(request);
    const title = `Owner deleted ${label}`;
    const { copy } = await shared(request, owner, learner, title);

    const ownerPage = await pageAs(browser, request, owner, mobile);
    await ownerPage.page.goto("/settings");
    await ownerPage.page
      .getByRole("button", { name: "Delete account" })
      .click();
    const dialog = ownerPage.page.getByRole("dialog", {
      name: "Delete your account?",
    });
    await expect(dialog).toContainText("learners who copied them keep");
    await dialog.getByLabel("Type DELETE to confirm").fill("DELETE");
    await dialog.getByRole("button", { name: "Delete permanently" }).click();
    await expect(ownerPage.page).toHaveURL(/sign-in/);
    await ownerPage.context.close();

    const before = await restore(request, learner, false);
    expect(before.status()).toBe(200);
    expect(await before.json()).toMatchObject({
      unresolved_pins: 0,
      pins: 1,
      reblocked: 0,
    });

    const { page, context } = await pageAs(browser, request, learner, mobile);
    await page.goto(`/course/${copy}`);
    await expect(page.getByRole("heading", { name: title })).toBeVisible();
    await expect(page.getByText("Created by deleted user")).toBeVisible();
    await expect(page.getByText("This copy keeps working")).toBeVisible();
    await noOverflow(page);
    await context.close();
    expect(await inCatalogue(request, title)).toBe(false);
  });

  test(`a severe takedown stays effective after restoring an older dump (${label})`, async ({
    browser,
    request,
  }) => {
    const owner = await account(request);
    const learner = await account(request);
    const title = `Severe restore ${label}`;
    const { course, copy } = await shared(request, owner, learner, title);
    for (const reason of ["unsafe", "unsafe", "spam"]) {
      await request.post(`${apiBase}/v1/marketplace/reports`, {
        headers: (await account(request)).headers,
        data: { target_type: "course", target_id: course, reason },
      });
    }
    const mod = await moderator(request);
    const queue = await (
      await request.get(`${apiBase}/v1/moderation/queue`, {
        headers: mod.headers,
      })
    ).json();
    const item = (queue.items as { id: string; target_id: string }[]).find(
      (i) => i.target_id === course,
    )!;
    const acted = await request.post(
      `${apiBase}/v1/moderation/items/${item.id}/actions`,
      {
        headers: mod.headers,
        data: {
          action: "takedown",
          reason_code: "unsafe",
          reason: "Lesson 3 gives instructions that could cause harm.",
        },
      },
    );
    expect(acted.status()).toBe(201);
    const blocked = async () =>
      (
        await (
          await request.get(`${apiBase}/v1/courses/${copy}`, {
            headers: learner.headers,
          })
        ).json()
      ).moderation_blocked;
    expect(await blocked()).toBe(true);

    // Restoring a dump from before the takedown clears the flag; the ledger puts it back.
    const rewound = await restore(request, mod, true);
    expect(rewound.status()).toBe(200);
    expect(await rewound.json()).toMatchObject({ reblocked: 1 });
    expect(await blocked()).toBe(true);

    const { page, context } = await pageAs(browser, request, learner, mobile);
    await page.goto(`/moderation/blocked/${copy}`);
    await expect(
      page.getByRole("heading", { name: "This course was removed" }),
    ).toBeVisible();
    await noOverflow(page);
    await context.close();
  });
}

test("unpublish dialog states: offline error recovers on retry, keyboard escape and reduced motion", async ({
  browser,
  request,
}) => {
  const owner = await account(request);
  const learner = await account(request);
  const { course } = await shared(request, owner, learner, "Unpublish states");
  const { page, context } = await pageAs(browser, request, owner, true);
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
  await page.goto(`/course/${course}`);
  await page.getByTestId("share-course").click();
  const panel = page.getByTestId("share-course-panel");
  await panel.getByRole("button", { name: "Unpublish course" }).click();
  await page.keyboard.press("Escape");
  await expect(
    page.getByRole("dialog", { name: "Unpublish this course?" }),
  ).toBeHidden();
  let fail = true;
  await page.route("**/v1/courses/*/unpublish", async (route) => {
    if (fail) {
      fail = false;
      await route.abort("failed");
      return;
    }
    await route.fallback();
  });
  await panel.getByRole("button", { name: "Unpublish course" }).click();
  await page
    .getByRole("dialog", { name: "Unpublish this course?" })
    .getByRole("button", { name: "Unpublish" })
    .click();
  await expect(page.getByRole("alert")).toContainText("offline");
  await page.getByRole("button", { name: "Retry" }).click();
  await expect(panel.getByRole("status").last()).toContainText("Unpublished");
  await context.close();
});
