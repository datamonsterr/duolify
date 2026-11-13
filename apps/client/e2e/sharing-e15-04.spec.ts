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
async function publish(
  request: APIRequestContext,
  headers: Headers,
  variant = "reuse",
) {
  const seeded = await request.post(`${apiBase}/test/sharing/course`, {
    headers,
    data: { variant },
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
    { headers, data: {} },
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
  (await (
    await request.get(`${apiBase}/v1/me/credits`, { headers })
  ).json()) as { available: number };
const fund = (request: APIRequestContext, headers: Headers, credits: number) =>
  request.post(`${apiBase}/test/sharing/fund`, { headers, data: { credits } });

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

/** Enroll a fresh learner and pass both published lessons: the end of published content. */
async function atTheEnd(
  browser: Browser,
  request: APIRequestContext,
  token: string,
  viewport?: { width: number; height: number },
) {
  const l = await learner(browser, request, viewport);
  const copy = (await (await enroll(request, l.headers, token)).json())
    .course_id as string;
  const lessons = (await courseOf(request, l.headers, copy)).units.flatMap(
    (u) => u.lessons,
  );
  await pass(request, l.headers, lessons[0]!.id);
  await pass(request, l.headers, lessons[1]!.id);
  return { ...l, copy, third: lessons[2]!.id, first: lessons[0]!.id };
}

const missing = (copy: string) =>
  `${apiBase}/v1/courses/${copy}/missing-lessons`;

test("a learner sees ready/total, a price-version quote, consents and generates the next lesson", async ({
  page,
  request,
  browser,
}) => {
  const owner = await tokenOf(page);
  const { course, token } = await publish(request, owner);
  const l = await atTheEnd(browser, request, token);
  expect((await fund(request, l.headers, 1000)).status()).toBe(204);
  const before = (await wallet(request, l.headers)).available;

  await l.page.goto(`/course/${l.copy}`);
  await expect(l.page.getByTestId("missing-ready")).toContainText(
    "2 of 3 lessons ready",
  );
  await expect(
    l.page.getByText(/Ready lessons are free for you/),
  ).toBeVisible();
  await l.page.getByRole("button", { name: "Review quote" }).click();

  const sheet = l.page.getByRole("dialog", { name: "Generate next lesson" });
  await expect(sheet).toBeVisible();
  await expect(sheet.getByTestId("quote-version")).toContainText(
    /Price version/,
  );
  await expect(sheet.getByTestId("quote-content")).toContainText(/credits/);
  await expect(sheet.getByTestId("quote-audio")).toContainText(/credits/);
  await expect(sheet.getByTestId("quote-total")).toContainText(
    /up to \d+ credits/,
  );
  const generate = sheet.getByRole("button", { name: "Generate lesson" });
  await expect(generate).toBeDisabled(); // consent is mandatory
  await sheet.getByRole("checkbox", { name: /I agree to spend up to/ }).check();
  await expect(generate).toBeEnabled();
  await generate.click();

  await expect(l.page.getByTestId("missing-status")).toContainText(
    "Lesson ready",
    { timeout: 30_000 },
  );
  await expect(l.page.getByTestId("missing-ready")).toContainText(
    "3 of 3 lessons ready",
  );
  const state = await courseOf(request, l.headers, l.copy);
  const third = state.units.flatMap((u) => u.lessons)[2]!;
  expect([third.state, third.content]).toEqual(["current", "ready"]);
  expect((await wallet(request, l.headers)).available).toBeLessThan(before);

  // Only this learner's copy changed: the snapshot and the owner's course are untouched.
  const preview = await (
    await request.get(`${apiBase}/v1/public/shares/${token}`)
  ).json();
  expect(JSON.stringify(preview)).toContain("2 of 3");
  const other = await atTheEnd(browser, request, token);
  const otherLessons = (
    await courseOf(request, other.headers, other.copy)
  ).units.flatMap((u) => u.lessons);
  expect(otherLessons[2]!.content).toBe("needs_generation");
  const ownerCourse = await courseOf(request, owner, course);
  expect(ownerCourse.units.flatMap((u) => u.lessons)[2]!.content).toBe(
    "needs_generation",
  );
  await l.context.close();
  await other.context.close();
});

test("gates: previous lesson not passed, no consent, stale quote and insufficient credits are refused", async ({
  page,
  request,
  browser,
}) => {
  const owner = await tokenOf(page);
  const { token } = await publish(request, owner);

  // Previous lesson not passed: no quote, no generation.
  const early = await learner(browser, request);
  const copy = (await (await enroll(request, early.headers, token)).json())
    .course_id as string;
  const status = await (
    await request.get(missing(copy), { headers: early.headers })
  ).json();
  expect(status).toMatchObject({ ready: 2, total: 3, state: "locked" });
  const refused = await request.post(`${missing(copy)}/quote`, {
    headers: early.headers,
    data: {},
  });
  expect(refused.status()).toBe(409);
  expect(await refused.json()).toEqual({ error: "previous_lesson_not_passed" });
  await early.page.goto(`/course/${copy}`);
  await expect(
    early.page.getByText(/Pass the previous lesson test/),
  ).toBeVisible();
  await expect(
    early.page.getByRole("button", { name: "Review quote" }),
  ).toHaveCount(0);

  // At the end: consent and balance are enforced server side, before the engine.
  const l = await atTheEnd(browser, request, token);
  const quote = await request.post(`${missing(l.copy)}/quote`, {
    headers: l.headers,
    data: {},
  });
  expect(quote.status()).toBe(201);
  const q = await quote.json();
  expect(q).toMatchObject({ lesson_id: l.third, sufficient: false });
  expect(q.pricing_version).toBeTruthy();
  const noConsent = await request.post(`${missing(l.copy)}/generate`, {
    headers: l.headers,
    data: { lesson_id: l.third, quote_id: q.quote_id, consent: false },
  });
  expect(noConsent.status()).toBe(400);
  expect(await noConsent.json()).toEqual({ error: "consent_required" });
  const poor = await request.post(`${missing(l.copy)}/generate`, {
    headers: l.headers,
    data: { lesson_id: l.third, quote_id: q.quote_id, consent: true },
  });
  expect(poor.status()).toBe(402);
  expect(await poor.json()).toEqual({ error: "insufficient_credits" });
  const stale = await request.post(`${missing(l.copy)}/generate`, {
    headers: l.headers,
    data: {
      lesson_id: l.third,
      quote_id: crypto.randomUUID(),
      consent: true,
    },
  });
  expect(stale.status()).toBe(409);
  expect(await stale.json()).toEqual({ error: "quote_stale" });

  // The sheet explains the shortfall and links to credit options.
  await l.page.goto(`/course/${l.copy}`);
  await l.page.getByRole("button", { name: "Review quote" }).click();
  const sheet = l.page.getByRole("dialog", { name: "Generate next lesson" });
  await expect(sheet.getByRole("alert")).toContainText(/Needs \d+ credits/);
  await expect(
    sheet.getByRole("link", { name: "See credit options" }),
  ).toBeVisible();
  await expect(
    sheet.getByRole("button", { name: "Generate lesson" }),
  ).toBeDisabled();
  await early.context.close();
  await l.context.close();
});

test("a failed generation releases the credits and offers a retry", async ({
  page,
  request,
  browser,
}) => {
  const owner = await tokenOf(page);
  const { token } = await publish(request, owner, "reuse_down");
  const l = await atTheEnd(browser, request, token);
  await fund(request, l.headers, 1000);
  const before = (await wallet(request, l.headers)).available;

  await l.page.goto(`/course/${l.copy}`);
  await l.page.getByRole("button", { name: "Review quote" }).click();
  const sheet = l.page.getByRole("dialog", { name: "Generate next lesson" });
  await sheet.getByRole("checkbox", { name: /I agree to spend up to/ }).check();
  await sheet.getByRole("button", { name: "Generate lesson" }).click();
  await expect(l.page.getByRole("alert")).toContainText(
    /Your reserved credits were released/,
    { timeout: 30_000 },
  );
  await expect(l.page.getByRole("button", { name: "Try again" })).toBeVisible();
  expect((await wallet(request, l.headers)).available).toBe(before);
  const lessons = (await courseOf(request, l.headers, l.copy)).units.flatMap(
    (u) => u.lessons,
  );
  expect(lessons[0]!.state).toBe("done"); // earlier lessons stay available
  expect(lessons[2]!.state).toBe("ready_to_generate");
  await l.context.close();
});

test("mobile: quote sheet with the keyboard, offline error recovers on retry", async ({
  page,
  request,
  browser,
}) => {
  const owner = await tokenOf(page);
  const { token } = await publish(request, owner);
  const l = await atTheEnd(browser, request, token, {
    width: 390,
    height: 780,
  });
  await fund(request, l.headers, 1000);
  await l.page.goto(`/course/${l.copy}`);
  let offline = true;
  await l.page.route("**/missing-lessons/quote", async (route) => {
    if (offline) return route.abort("failed");
    return route.fallback();
  });
  const review = l.page.getByRole("button", { name: "Review quote" });
  await review.focus();
  await l.page.keyboard.press("Enter");
  await expect(l.page.getByRole("alert")).toContainText(
    /Couldn't reach Duolify/,
  );
  offline = false;
  await l.page.getByRole("button", { name: "Retry" }).click();
  const sheet = l.page.getByRole("dialog", { name: "Generate next lesson" });
  await expect(sheet).toBeVisible();
  const box = await sheet.boundingBox();
  expect(box!.width).toBeLessThanOrEqual(390); // a bottom sheet, not a side panel
  await l.page.keyboard.press("Escape");
  await expect(sheet).toHaveCount(0);
  await expect(review).toBeFocused();
  await l.context.close();
});
