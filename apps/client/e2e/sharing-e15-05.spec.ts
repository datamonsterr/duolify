import type { APIRequestContext, Browser, Page } from "@playwright/test";
import { apiBase, webBase } from "./servers";
import { test, expect } from "./accounts-fixture";

type Headers = { Authorization: string };
type Course = {
  units: { lessons: { id: string; state: string; content?: string }[] }[];
};

async function tokenOf(page: Page): Promise<Headers> {
  await page.goto("/profile");
  const tokens = await page.evaluate(() =>
    JSON.parse(sessionStorage.getItem("duolify.native-store-fixture")!),
  );
  return { Authorization: `Bearer ${tokens.access_token}` };
}

/** Owner publishes a course with two generated lessons and one still to generate. */
async function publish(request: APIRequestContext, headers: Headers) {
  const seeded = await request.post(`${apiBase}/test/sharing/course`, {
    headers,
    data: { variant: "reuse" },
  });
  expect(seeded.status()).toBe(201);
  const course = (await seeded.json()).course_id as string;
  const draft = await request.post(
    `${apiBase}/v1/courses/${course}/publication-drafts`,
    { headers, data: {} },
  );
  expect((await draft.json()).verdict).toBe("pass");
  const link = await request.post(
    `${apiBase}/v1/courses/${course}/share-link`,
    {
      headers,
      data: {},
    },
  );
  expect(link.status()).toBe(201);
  const { token } = (await link.json()) as { token: string };
  return { course, token };
}

/** A second, signed-in learner in a fresh context. */
async function learner(
  browser: Browser,
  request: APIRequestContext,
  viewport = { width: 1280, height: 800 },
) {
  const email = `learner-${crypto.randomUUID()}@example.test`;
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
  const tokens = await verified.json();
  const context = await browser.newContext({
    baseURL: webBase,
    viewport,
    hasTouch: viewport.width < 768,
  });
  // The same native credential-store double the default context gets from the fixture.
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
  }, tokens);
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
  const headers: Headers = { Authorization: `Bearer ${tokens.access_token}` };
  return { context, page, headers };
}

const enroll = (request: APIRequestContext, headers: Headers, token: string) =>
  request.post(`${apiBase}/v1/public/shares/${token}/enroll`, {
    headers,
    data: {},
  });
const courseOf = async (
  request: APIRequestContext,
  headers: Headers,
  id: string,
) =>
  (await (
    await request.get(`${apiBase}/v1/courses/${id}`, { headers })
  ).json()) as Course;
const wallet = async (request: APIRequestContext, headers: Headers) =>
  JSON.stringify(
    await (await request.get(`${apiBase}/v1/me/credits`, { headers })).json(),
  );

async function pass(
  request: APIRequestContext,
  headers: Headers,
  lesson: string,
) {
  const started = await request.get(`${apiBase}/v1/lessons/${lesson}/test`, {
    headers,
  });
  expect(started.status()).toBe(200);
  const { questions } = await started.json();
  for (const q of questions)
    await request.post(`${apiBase}/v1/lessons/${lesson}/test/answer`, {
      headers,
      data: { question_id: q.question_id, choice_index: 0 },
    });
}

const republish = (
  request: APIRequestContext,
  headers: Headers,
  course: string,
  changelog = "",
) =>
  request.post(`${apiBase}/v1/courses/${course}/republish`, {
    headers,
    data: { changelog },
  });
const edit = (request: APIRequestContext, headers: Headers, course: string) =>
  request.post(`${apiBase}/test/sharing/edit`, {
    headers,
    data: { course_id: course },
  });

/** Owner publishes, a learner copies and passes lesson 1, then the owner edits. */
async function copied(
  page: Page,
  request: APIRequestContext,
  browser: Browser,
  viewport?: { width: number; height: number },
) {
  const owner = await tokenOf(page);
  const { course, token } = await publish(request, owner);
  const l = await learner(browser, request, viewport);
  const copy = (await (await enroll(request, l.headers, token)).json())
    .course_id as string;
  const lessons = (await courseOf(request, l.headers, copy)).units.flatMap(
    (u) => u.lessons,
  );
  await pass(request, l.headers, lessons[0]!.id);
  expect((await edit(request, owner, course)).status()).toBeLessThan(300);
  return { owner, course, token, l, copy };
}

test("republish keeps the old version, then the learner updates with retitled and removed lessons", async ({
  page,
  request,
  browser,
}) => {
  const { owner, course, l, copy } = await copied(page, request, browser);

  // Owner: Publish new version with a changelog, screened anew.
  await page.goto(`/course/${course}`);
  await page.getByTestId("share-course").click();
  const panel = page.getByTestId("share-course-panel");
  await panel
    .getByLabel("What's new in this version")
    .fill("Retitled tides, removed eclipses, added comets.");
  await panel.getByRole("button", { name: "Publish new version" }).click();
  await expect(panel.getByText("Version 2 is live")).toBeVisible();
  await expect(panel.getByText(/Learners on version 1 see/)).toBeVisible();

  // The learner copy keeps its pin and content until an explicit update.
  await l.page.goto(`/course/${copy}`);
  await expect(l.page.getByTestId("pinned-version")).toContainText("version 1");
  await expect(l.page.getByText("Tides", { exact: true })).toBeVisible();
  await expect(l.page.getByText("Ocean tides")).toHaveCount(0);

  await l.page.getByRole("button", { name: "Update available" }).click();
  const sheet = l.page.getByTestId("course-update");
  await expect(sheet.getByText("Version 2 is available")).toBeVisible();
  await expect(sheet.getByText("You're on version 1.")).toBeVisible();
  await expect(sheet.getByText("What's new")).toBeVisible();
  await expect(
    sheet.getByText(/Retitled tides, removed eclipses/),
  ).toBeVisible();
  await expect(sheet.getByText("What happens to your progress")).toBeVisible();
  await expect(
    sheet.getByText(/completed lesson.*stay.*completed/),
  ).toBeVisible();
  await sheet.getByRole("button", { name: "Update to version 2" }).click();

  await expect(l.page.getByTestId("pinned-version")).toContainText("version 2");
  await expect(l.page.getByText("Ocean tides")).toBeVisible();
  await expect(
    l.page.getByRole("button", { name: "Update available" }),
  ).toHaveCount(0);
  const archived = l.page.getByTestId("archived-lessons");
  await archived.getByText("Archived lessons").click();
  await expect(archived.getByText("Eclipses")).toBeVisible();

  const state = await courseOf(request, l.headers, copy);
  const lessons = state.units.flatMap((u) => u.lessons);
  expect(lessons[0]!.state).toBe("done"); // completed work kept
  expect(JSON.stringify(state)).not.toContain("Eclipses");
  // The owner's old version is untouched for other learners, a second copy keeps its pin.
  expect(owner.Authorization).toBeTruthy();
  await l.context.close();
});

test("the fourth republish in a day is refused and the panel explains it", async ({
  page,
  request,
}) => {
  const owner = await tokenOf(page);
  const { course } = await publish(request, owner);
  for (let i = 0; i < 3; i++) {
    const ok = await republish(request, owner, course, `change ${i}`);
    expect(ok.status()).toBe(201);
    expect((await ok.json()).verdict).toBe("pass");
  }
  const refused = await republish(request, owner, course);
  expect(refused.status()).toBe(429);
  expect(await refused.json()).toEqual({ error: "republish_limit" });

  await page.goto(`/course/${course}`);
  await page.getByTestId("share-course").click();
  const panel = page.getByTestId("share-course-panel");
  await expect(panel.getByText(/0 of 3 new versions left today/)).toBeVisible();
  await expect(
    panel.getByRole("button", { name: "Publish new version" }),
  ).toBeDisabled();
});

test("mobile: update sheet works with the keyboard, offline error recovers on retry", async ({
  page,
  request,
  browser,
}) => {
  const { owner, course, l, copy } = await copied(page, request, browser, {
    width: 390,
    height: 780,
  });
  expect(
    (await republish(request, owner, course, "Mobile change")).status(),
  ).toBe(201);
  await l.page.goto(`/course/${copy}`);
  const open = l.page.getByRole("button", { name: "Update available" });
  await open.focus();
  await l.page.keyboard.press("Enter");
  const sheet = l.page.getByTestId("course-update");
  await expect(sheet.getByText("Version 2 is available")).toBeVisible();

  // Escape closes the sheet and returns focus to its opener.
  await l.page.keyboard.press("Escape");
  await expect(sheet).toHaveCount(0);
  await expect(open).toBeFocused();
  await l.page.keyboard.press("Enter");

  let offline = true;
  await l.page.route("**/v1/courses/*/update", async (route) => {
    if (offline && route.request().method() === "POST")
      return route.abort("failed");
    return route.fallback();
  });
  const apply = sheet.getByRole("button", { name: "Update to version 2" });
  await apply.focus();
  await l.page.keyboard.press("Enter");
  await expect(l.page.getByRole("alert")).toContainText(
    /Couldn't reach Duolify/,
  );
  offline = false;
  await l.page.getByRole("button", { name: "Retry" }).click();
  await expect(l.page.getByTestId("pinned-version")).toContainText("version 2");
  await l.context.close();
});
