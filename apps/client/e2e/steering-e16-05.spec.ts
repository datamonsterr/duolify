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

const instructions = (copy: string) =>
  `${apiBase}/v1/courses/${copy}/instructions`;

for (const mobile of [false, true]) {
  test(`E16-05 fork warning: decline, then accept and generate the first private future lesson ${mobile ? "mobile" : "desktop"}`, async ({
    page,
    request,
    browser,
  }) => {
    const owner = await tokenOf(page);
    const { token } = await publish(request, owner);
    const l = await learner(
      browser,
      request,
      mobile ? { width: 390, height: 844 } : { width: 1280, height: 900 },
    );
    await l.page.emulateMedia({
      colorScheme: mobile ? "dark" : "light",
      reducedMotion: "reduce",
    });
    const copy = (await (await enroll(request, l.headers, token)).json())
      .course_id as string;
    expect((await fund(request, l.headers, 1000)).status()).toBe(204);
    const before = await courseOf(request, l.headers, copy);
    const lessons = before.units.flatMap((u) => u.lessons);
    expect(lessons.map((x) => x.content)).toEqual([
      "ready",
      "ready",
      "needs_generation",
    ]);

    await l.page.goto(`/course/${copy}`);
    await l.page.getByRole("button", { name: "Steer course" }).click();
    if (mobile)
      await l.page.getByRole("button", { name: "Add instruction" }).click();
    const input = l.page.getByLabel("New instruction");
    await input.fill("More examples in Go");
    const save = l.page.getByRole("button", { name: "Save instruction" });
    await save.click();

    // Decline: nothing is saved, the copy is not forked and reuse continues.
    const dialog = l.page.getByRole("dialog", {
      name: "Make this course your own?",
    });
    await expect(dialog).toBeVisible();
    await expect(dialog).toContainText(/cost credits/i);
    await expect(dialog).toContainText(/won.t receive/i);
    if (mobile) await dialog.getByRole("button", { name: "Cancel" }).click();
    else await l.page.keyboard.press("Escape");
    await expect(dialog).toBeHidden();
    await expect(input).toHaveValue("More examples in Go");
    await expect(save).toBeFocused();
    const declined = await (
      await request.get(instructions(copy), { headers: l.headers })
    ).json();
    expect(declined.instructions).toEqual([]);
    expect(declined.fork).toMatchObject({ forked: false, required: true });
    expect(
      (await courseOf(request, l.headers, copy)).units.flatMap((u) =>
        u.lessons.map((x) => x.content),
      ),
    ).toEqual(["ready", "ready", "needs_generation"]);

    // Accept.
    await save.click();
    await l.page.getByRole("button", { name: "Make it mine" }).click();
    await expect(l.page.getByText("Active", { exact: true })).toBeVisible();
    const forked = await (
      await request.get(instructions(copy), { headers: l.headers })
    ).json();
    expect(forked.fork).toMatchObject({ forked: true, required: false });
    expect(forked.instructions).toHaveLength(1);
    const after = (await courseOf(request, l.headers, copy)).units.flatMap(
      (u) => u.lessons,
    );
    // The unlocked first lesson stays; later published lessons are no longer reused.
    expect(after.map((x) => x.content)).toEqual([
      "ready",
      "needs_generation",
      "needs_generation",
    ]);

    // Pass the first lesson: the next lesson now waits to be generated for this learner.
    await pass(request, l.headers, after[0]!.id);
    const wallet0 = (await wallet(request, l.headers)).available;
    await l.page.goto(`/course/${copy}`);
    await expect(l.page.getByTestId("missing-ready")).toContainText(
      "1 of 3 lessons ready",
    );
    await l.page.getByRole("button", { name: "Review quote" }).click();
    const sheet = l.page.getByRole("dialog", { name: "Generate next lesson" });
    await sheet
      .getByRole("checkbox", { name: /I agree to spend up to/ })
      .check();
    await sheet.getByRole("button", { name: "Generate lesson" }).click();
    await expect(l.page.getByTestId("missing-status")).toContainText(
      "Lesson ready",
      { timeout: 30_000 },
    );
    expect((await wallet(request, l.headers)).available).toBeLessThan(wallet0);
    const generated = (await courseOf(request, l.headers, copy)).units.flatMap(
      (u) => u.lessons,
    );
    expect(generated.map((x) => x.content)).toEqual([
      "ready",
      "ready",
      "needs_generation",
    ]);
    // The approved instruction was applied to the generated lesson.
    await l.page.getByRole("button", { name: "Steer course" }).click();
    await expect(l.page.getByText(/Applied to lesson/)).toBeVisible();

    // Deleting every instruction never rejoins the published course.
    const list = await (
      await request.get(instructions(copy), { headers: l.headers })
    ).json();
    for (const i of list.instructions)
      await request.delete(`${instructions(copy)}/${i.id}`, {
        headers: l.headers,
      });
    const rejoined = await (
      await request.get(instructions(copy), { headers: l.headers })
    ).json();
    expect(rejoined.fork).toMatchObject({ forked: true });
    await l.context.close();
  });
}
