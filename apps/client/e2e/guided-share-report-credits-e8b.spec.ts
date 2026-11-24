import type { APIRequestContext, Locator, Page } from "@playwright/test";
import { apiBase } from "./servers";
import { test, expect, resetTokens } from "./accounts-fixture";

// Share, invite, republish, review, report and credits consent as chats (flag `rest`, default
// on). The old forms are covered by the specs that switch the flag off.
// Set APPROVAL_DIR to also write the owner-gate screenshots (start, mid with widget, done).
const dir = process.env.APPROVAL_DIR;
const shot = async (
  page: Page,
  flow: string,
  step: string,
  tag: string,
  chat?: ReturnType<Page["locator"]>,
) => {
  if (!dir) return;
  // Long sheets: bring the chat being shown to the bottom of the view.
  await chat?.evaluate((el) => el.scrollIntoView({ block: "end" }));
  await page.waitForTimeout(1200); // bubble animation and mascot chunk settle
  await page.screenshot({ path: `${dir}/${flow}-${tag}-${step}.png` });
};
const send = (scope: ReturnType<Page["locator"]>) =>
  scope
    .locator(".gc-widget")
    .getByRole("button", { name: "Send", exact: true })
    .click();
/** A guide bubble (the screen-reader log repeats each question, so match the bubble only). */
const said = (scope: ReturnType<Page["locator"]>, text: string) =>
  scope.locator(".guided-bubble", { hasText: text }).first();
const noOverflow = async (page: Page) =>
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth > window.innerWidth + 1,
    ),
  ).toBe(false);

type Headers = { Authorization: string };
async function signIn(request: APIRequestContext, who: string) {
  const email = `${who}-${crypto.randomUUID()}@example.test`;
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
  return { Authorization: `Bearer ${(await verified.json()).access_token}` };
}
const social = (
  request: APIRequestContext,
  headers: Headers,
  method: string,
  path: string,
  data?: unknown,
) => request.fetch(`${apiBase}/v1/social${path}`, { method, headers, data });

async function learner(request: APIRequestContext, name: string) {
  const headers = await signIn(request, "e8b");
  expect(
    (
      await social(request, headers, "PUT", "/username", { username: name })
    ).status(),
  ).toBe(200);
  return headers;
}

async function me(page: Page, request: APIRequestContext, fallback: string) {
  const token = await page.evaluate(
    () =>
      JSON.parse(sessionStorage.getItem("duolify.native-store-fixture") ?? "{}")
        .access_token as string,
  );
  const headers = { Authorization: `Bearer ${token}` };
  const identity = await (
    await social(request, headers, "GET", "/identity")
  ).json();
  if (!identity.username) {
    await social(request, headers, "PUT", "/username", { username: fallback });
    return { headers, name: fallback };
  }
  return { headers, name: identity.username.username as string };
}

async function befriend(
  request: APIRequestContext,
  from: Headers,
  to: Headers,
  toName: string,
) {
  const sent = await (
    await social(request, from, "POST", "/friend-requests", {
      username: toName,
    })
  ).json();
  expect(
    (
      await social(request, to, "POST", `/friend-requests/${sent.id}/accept`)
    ).status(),
  ).toBe(200);
}

async function seedCourse(request: APIRequestContext, headers: Headers) {
  const response = await request.post(`${apiBase}/test/marketplace/course`, {
    headers,
    data: { title: "Guided share course", description: "Small steps." },
  });
  expect(response.status()).toBe(201);
  return (await response.json()).course_id as string;
}

/** The web build talks to its own API origin; the disposable API listens elsewhere. */
async function forward(page: Page, request: APIRequestContext) {
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
}

async function listed(request: APIRequestContext) {
  const headers = await signIn(request, "owner");
  const seeded = await request.post(`${apiBase}/test/marketplace/course`, {
    headers,
    data: { title: "Guided review course", description: "Short lessons" },
  });
  expect(seeded.status()).toBe(201);
  const course = (await seeded.json()).course_id as string;
  const draft = await request.post(
    `${apiBase}/v1/courses/${course}/publication-drafts`,
    { headers, data: {} },
  );
  expect((await draft.json()).verdict).toBe("pass");
  const put = await request.put(
    `${apiBase}/v1/courses/${course}/marketplace-listing`,
    { headers, data: { category: "tech", language: "en", tags: [] } },
  );
  expect(put.status()).toBe(200);
  return course;
}

const courseId = "c1111111-1111-4111-8111-111111111111";
const lessonId = "l1111111-1111-4111-8111-111111111111";
async function mockQuoteCourse(page: Page) {
  const calls = { accept: 0, prepare: 0 };
  let state = "ready_to_generate";
  let accepted = false;
  const course = () => ({
    id: courseId,
    title: "Astronomy for curious beginners",
    description: "Explore the night sky.",
    units: [
      {
        id: "u1",
        title: "Finding your bearings",
        lessons: [
          { id: lessonId, title: "A map of the night", goal: "Sky", state },
        ],
      },
    ],
  });
  await page.route("**/v1/features", (r) =>
    r.fulfill({ json: { lesson_test_tracer_enabled: true } }),
  );
  await page.route("**/v1/courses", (r) =>
    r.fulfill({ json: { courses: [course()], active_course_id: courseId } }),
  );
  await page.route(`**/v1/courses/${courseId}`, (r) =>
    r.fulfill({ json: course() }),
  );
  await page.route("**/v1/quotes", (r) =>
    r.fulfill({
      status: 201,
      json: {
        id: "quote-8b",
        action: "lesson_content",
        pricing_version: "usd-quotes-2026-11-09",
        currency: "USD",
        content_ceiling: 137,
        audio_ceiling: 0,
        daily_free_eligible: false,
        expires_at: new Date(Date.now() + 600000).toISOString(),
        accepted: false,
      },
    }),
  );
  await page.route("**/v1/quotes/quote-8b/accept", (r) => {
    calls.accept++;
    accepted = true;
    return r.fulfill({ json: { accepted: true } });
  });
  await page.route(`**/v1/lessons/${lessonId}/prepare`, (r) => {
    calls.prepare++;
    if (accepted) state = "generating";
    return r.fulfill({
      status: accepted ? 202 : 409,
      json: accepted ? { id: "gen-8b", state: "generating" } : { error: "q" },
    });
  });
  await page.route("**/v1/lesson-generations/gen-8b**", (r) => {
    state = "current";
    return r.fulfill({ json: { id: "gen-8b", state: "completed" } });
  });
  return calls;
}

/** Close is the shared 44 px × icon button: named "Close", no visible text. */
async function iconClose(scope: Locator) {
  const close = scope.getByRole("button", { name: /^Close/ });
  await expect(close).toHaveCount(1);
  await expect(close).toHaveText("×");
  const box = await close.boundingBox();
  expect(box?.width ?? 0).toBeGreaterThanOrEqual(44);
  expect(box?.height ?? 0).toBeGreaterThanOrEqual(44);
}

test.afterEach(() => {
  resetTokens();
});

for (const [width, height, scheme] of [
  [390, 844, "light"],
  [390, 844, "dark"],
  [1280, 800, "light"],
  [1280, 800, "dark"],
] as const) {
  const tag = `${width}-${scheme}`;
  test.describe(`${tag}`, () => {
    test.beforeEach(async ({ page }) => {
      await page.setViewportSize({ width, height });
      await page.emulateMedia({ colorScheme: scheme });
    });

    test("share, invite and republish as chats", async ({ page, request }) => {
      await forward(page, request);
      await page.goto("/profile");
      const p = `g${crypto.randomUUID().replaceAll("-", "").slice(0, 8)}`;
      const mine = await me(page, request, `${p}.own`);
      const sam = await learner(request, `${p}.sam`);
      await befriend(request, sam, mine.headers, mine.name);
      const course = await seedCourse(request, mine.headers);
      await page.goto(`/course/${course}`);
      await page.getByTestId("share-course").click();
      const panel = page.getByTestId("share-course-panel");
      await iconClose(page.getByRole("dialog", { name: "Share course" }));

      // course.share: the old radio group is gone; the chat opens on the current answer.
      await expect(panel.getByRole("radio", { name: "Private" })).toHaveCount(
        0,
      );
      await expect(said(panel, "Who can open this course?")).toBeVisible();
      await shot(
        page,
        "share",
        "start",
        tag,
        panel.locator('[data-script="course.share"]'),
      );
      await panel
        .getByRole("button", { name: "Change answer: Only me" })
        .click();
      await expect(panel.getByRole("radio")).toHaveCount(2);
      await shot(
        page,
        "share",
        "mid",
        tag,
        panel.locator('[data-script="course.share"]'),
      );
      await panel.locator("label", { hasText: "Anyone with the link" }).click();
      await panel.getByRole("button", { name: "Apply" }).click();
      await expect(panel.getByTestId("share-link-url")).toBeVisible();
      await expect(panel.getByText("Sharing updated.")).toBeVisible();
      await shot(
        page,
        "share",
        "done",
        tag,
        panel.locator('[data-script="course.share"]'),
      );

      // course.invite
      await expect(said(panel, "Which friend?")).toBeVisible();
      const field = panel.locator(".gc-widget").getByRole("textbox");
      await shot(
        page,
        "invite",
        "start",
        tag,
        panel.locator('[data-script="course.invite"]'),
      );
      await field.press("Enter");
      await expect(panel.getByRole("alert")).toBeVisible();
      await field.fill(`@${p}.sam`);
      await send(panel);
      await expect(said(panel, "Send this invite?")).toBeVisible();
      await iconClose(
        page.getByRole("dialog", { name: /Share course|publication/i }),
      );
      await shot(
        page,
        "invite",
        "mid",
        tag,
        panel.locator('[data-script="course.invite"]'),
      );
      await panel.getByRole("button", { name: "Send invite" }).click();
      await expect(panel.getByTestId("invite-notice")).toHaveText(
        "Invite sent",
      );
      await expect(panel.getByText("Invite sent.")).toBeVisible();
      await expect(
        panel
          .getByRole("list", { name: "Sent invites" })
          .getByTestId(`sent-invite-${p}.sam`),
      ).toContainText("Pending");
      await shot(
        page,
        "invite",
        "done",
        tag,
        panel.locator('[data-script="course.invite"]'),
      );

      // course.republish
      await panel.getByRole("button", { name: "New version" }).click();
      await expect(said(panel, "What's new in this version?")).toBeVisible();
      await shot(
        page,
        "republish",
        "start",
        tag,
        panel.locator('[data-script="course.republish"]'),
      );
      await panel.locator(".gc-widget textarea").fill("Retitled tides.");
      await iconClose(
        page.getByRole("dialog", { name: /Share course|publication/i }),
      );
      await shot(
        page,
        "republish",
        "mid",
        tag,
        panel.locator('[data-script="course.republish"]'),
      );
      await send(panel);
      await panel.getByRole("button", { name: "Publish", exact: true }).click();
      await expect(panel.getByText("Version 2 is live")).toBeVisible();
      await shot(
        page,
        "republish",
        "done",
        tag,
        panel.locator('[data-script="course.republish"]'),
      );
      await noOverflow(page);
    });

    test("report a course as a chat", async ({ page, request }) => {
      const course = await listed(request);
      await forward(page, request);
      await page.goto(`/marketplace/${course}`);
      await page.getByRole("button", { name: "More: report course" }).click();
      await page.getByRole("menuitem", { name: "Report this course" }).click();
      const sheet = page.getByRole("dialog", { name: "Report this course" });
      await expect(said(sheet, "What's wrong?")).toBeVisible();
      await expect(sheet.getByRole("radio", { name: "Spam" })).toBeVisible();
      await shot(page, "report", "start", tag);
      await sheet.locator("label", { hasText: "Copyright" }).click();
      const details = sheet.locator(".gc-widget textarea");
      await expect(details).toBeVisible();
      await details.fill("x".repeat(501));
      await send(sheet);
      await expect(sheet.getByRole("alert")).toHaveText(
        "Keep it under 500 characters.",
      );
      await expect(sheet.locator(".gc-count")).toHaveText("501/500");
      await iconClose(sheet);
      await shot(page, "report", "mid", tag);
      await details.fill("Lesson 4 copies a textbook chapter.");
      await send(sheet);
      await expect(said(sheet, "never told")).toBeVisible();
      await sheet.getByRole("button", { name: "Send report" }).click();
      await expect(sheet.getByRole("status")).toContainText(
        "Thanks — report sent",
      );
      await shot(page, "report", "done", tag);
      await noOverflow(page);
    });

    test("review a course as a chat", async ({ page, request }) => {
      const course = await listed(request);
      let auth = "";
      await page.route("**/v1/**", async (route) => {
        auth = route.request().headers().authorization || auth;
        const target = new URL(route.request().url());
        target.host = new URL(apiBase).host;
        const headers = route.request().headers();
        delete headers.origin;
        await route.fulfill({
          response: await request.fetch(target.href, {
            method: route.request().method(),
            headers,
            data: route.request().postData() ?? undefined,
          }),
        });
      });
      await page.goto(`/marketplace/${course}`);
      await page.getByRole("button", { name: /^Enroll/ }).click();
      await expect(
        page.getByRole("heading", { name: "Reviews", exact: true }),
      ).toBeVisible();
      const headers = { Authorization: auth };
      const en = await request.post(
        `${apiBase}/v1/marketplace/courses/${course}/enroll`,
        { headers, data: {} },
      );
      const copy = (await en.json()).course_id;
      const lessons = (
        await (
          await request.get(`${apiBase}/v1/courses/${copy}`, { headers })
        ).json()
      ).units.flatMap((u: { lessons: { id: string }[] }) => u.lessons);
      for (const lesson of lessons.slice(0, 3)) {
        const started = await request.get(
          `${apiBase}/v1/lessons/${lesson.id}/test`,
          { headers },
        );
        for (const q of (await started.json()).questions)
          await request.post(`${apiBase}/v1/lessons/${lesson.id}/test/answer`, {
            headers,
            data: { question_id: q.question_id, choice_index: 0 },
          });
      }
      await page.reload();
      const reviews = page.getByTestId("course-reviews");
      await expect(said(reviews, "How many stars?")).toBeVisible();
      await expect(reviews.getByLabel("Review text")).toHaveCount(0);
      await shot(
        page,
        "review",
        "start",
        tag,
        reviews.locator('[data-script="course.review"]'),
      );
      await reviews.locator("label", { hasText: "5 ★" }).click();
      await expect(said(reviews, "Anything to add?")).toBeVisible();
      await reviews.locator(".gc-widget textarea").fill("Helpful and clear");
      await shot(
        page,
        "review",
        "mid",
        tag,
        reviews.locator('[data-script="course.review"]'),
      );
      await send(reviews);
      await reviews.getByRole("button", { name: "Post review" }).click();
      await expect(page.getByTestId("review-status")).toContainText(
        "Review saved",
      );
      await expect(reviews).toContainText("Helpful and clear");
      await shot(
        page,
        "review",
        "done",
        tag,
        reviews.locator('[data-script="course.review"]'),
      );
      await noOverflow(page);
    });

    test("accept quote terms only on the explicit confirm", async ({
      page,
    }) => {
      const calls = await mockQuoteCourse(page);
      await page.goto(`/course/${courseId}`);
      await page.getByRole("button", { name: /Prepare lesson/i }).click();
      const dialog = page.getByRole("dialog");
      await expect(
        dialog.getByText("Content ceiling: 137 credits"),
      ).toBeVisible();
      await expect(said(dialog, "Accept these terms?")).toBeVisible();
      await iconClose(dialog);
      await expect(
        dialog.getByRole("button", { name: "Accept quote terms" }),
      ).toHaveCount(0);
      expect(calls.accept).toBe(0);
      expect(calls.prepare).toBe(0);
      await shot(page, "credits", "start", tag);
      await shot(page, "credits", "mid", tag);
      await dialog.getByRole("button", { name: "Accept terms" }).click();
      await expect(
        page.getByRole("link", { name: /Start lesson/i }),
      ).toBeVisible();
      expect(calls.accept).toBe(1);
      await shot(page, "credits", "done", tag);
    });
  });
}
