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

test("a learner enrolls twice and unlocks a reused lesson without spending credits", async ({
  page,
  request,
  browser,
}) => {
  const owner = await tokenOf(page);
  const { course, token } = await publish(request, owner);
  const l = await learner(browser, request);
  const before = await wallet(request, l.headers);

  await l.page.goto(`/c/${token}`);
  await expect(l.page.getByTestId("lessons-ready")).toContainText(
    "2 of 3 lessons ready",
  );
  await l.page.getByRole("button", { name: "Enroll" }).click();
  await expect(l.page.getByTestId("enroll-status")).toContainText("Enrolled");
  const open = l.page.getByRole("link", { name: "Open course" });
  await expect(open).toHaveAttribute("href", /\/course\/[0-9a-f-]{36}$/);
  const copy = (await open.getAttribute("href"))!.split("/").pop()!;

  // Enrolling again (another tab, API) returns the same copy and creates nothing.
  const again = await enroll(request, l.headers, token);
  expect(again.status()).toBe(200);
  expect(await again.json()).toMatchObject({ course_id: copy, created: false });
  await l.page.reload();
  await expect(l.page.getByTestId("enroll-status")).toContainText(
    "Already enrolled",
  );
  const mine = await (
    await request.get(`${apiBase}/v1/courses`, { headers: l.headers })
  ).json();
  expect(JSON.stringify(mine).split(copy).length - 1).toBeGreaterThanOrEqual(1);

  // The gate still holds: lesson 2 stays locked until lesson 1 is passed.
  let state = await courseOf(request, l.headers, copy);
  const [one, two, three] = state.units.flatMap((u) => u.lessons);
  expect([one!.state, two!.state, three!.state]).toEqual([
    "current",
    "locked",
    "locked",
  ]);
  expect([one!.content, three!.content]).toEqual(["ready", "needs_generation"]);

  // Manifests carry slides but never the answer key or explanations.
  const manifest = await request.get(
    `${apiBase}/v1/lessons/${one!.id}/manifest`,
    { headers: l.headers },
  );
  expect(manifest.status()).toBe(200);
  const text = await manifest.text();
  for (const banned of ["answer_index", "Relative angles", "Sun angle"])
    expect(text).not.toContain(banned);

  await pass(request, l.headers, one!.id);
  state = await courseOf(request, l.headers, copy);
  const lessons = state.units.flatMap((u) => u.lessons);
  expect([lessons[0]!.state, lessons[1]!.state]).toEqual(["done", "current"]);
  expect(await wallet(request, l.headers)).toBe(before); // zero credits

  // The creator sees an aggregate count, never the learner's copy.
  const preview = await (
    await request.get(`${apiBase}/v1/public/shares/${token}`)
  ).json();
  expect(preview.enrollment_count).toBe(1);
  const peek = await request.get(`${apiBase}/v1/courses/${copy}`, {
    headers: owner,
  });
  expect(peek.status()).toBe(404);
  expect(course).not.toBe(copy);
  await l.context.close();
});

test("a revoked link refuses new enrollment while existing copies keep working", async ({
  page,
  request,
  browser,
}) => {
  const owner = await tokenOf(page);
  const { course, token } = await publish(request, owner);
  const first = await learner(browser, request);
  const copy = (await (await enroll(request, first.headers, token)).json())
    .course_id as string;
  expect(
    (
      await request.delete(`${apiBase}/v1/courses/${course}/share-link`, {
        headers: owner,
      })
    ).status(),
  ).toBeLessThan(300);

  const second = await learner(browser, request);
  const refused = await enroll(request, second.headers, token);
  expect(refused.status()).toBe(404);
  expect(await refused.json()).toEqual({ error: "link_not_found" });
  await second.page.goto(`/c/${token}`);
  await expect(second.page.getByText(/no longer active/)).toBeVisible();
  await expect(second.page.getByRole("button", { name: "Enroll" })).toHaveCount(
    0,
  );

  const kept = await courseOf(request, first.headers, copy);
  expect(kept.units.length).toBeGreaterThan(0);
  await first.context.close();
  await second.context.close();
});

test("mobile: enroll with the keyboard, offline error recovers on retry", async ({
  page,
  request,
  browser,
}) => {
  const owner = await tokenOf(page);
  const { token } = await publish(request, owner);
  const l = await learner(browser, request, { width: 390, height: 780 });
  await l.page.goto(`/c/${token}`);
  let offline = true;
  await l.page.route("**/enroll", async (route, req) => {
    if (offline) return route.abort("failed");
    return route.fallback();
  });
  const button = l.page.getByRole("button", { name: "Enroll" });
  await button.focus();
  await l.page.keyboard.press("Enter");
  await expect(l.page.getByRole("alert")).toContainText(
    /Couldn't reach Duolify/,
  );
  offline = false;
  await l.page.getByRole("button", { name: "Retry" }).click();
  await expect(l.page.getByTestId("enroll-status")).toContainText("Enrolled");
  await expect(l.page.getByText(/Ready lessons are free/)).toBeVisible();
  await l.context.close();
});
