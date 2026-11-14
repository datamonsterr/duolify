import type { APIRequestContext, Page } from "@playwright/test";
import { apiBase } from "./servers";
import { test, expect } from "./accounts-fixture";

type Headers = { Authorization: string };

/** A fresh signed-in account (the owner of one catalogue course), reached through the real API. */
async function account(request: APIRequestContext): Promise<Headers> {
  const email = `owner-${crypto.randomUUID()}@example.test`;
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
  return { Authorization: `Bearer ${tokens.access_token}` };
}

/** Seeds a 4-lesson course (3 generated), screens it and lists it in the Marketplace. */
async function listed(
  request: APIRequestContext,
  fields: {
    title: string;
    description: string;
    category: string;
    language: string;
    tags: string[];
  },
) {
  const headers = await account(request);
  const seeded = await request.post(`${apiBase}/test/marketplace/course`, {
    headers,
    data: { title: fields.title, description: fields.description },
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
    {
      headers,
      data: {
        category: fields.category,
        language: fields.language,
        tags: fields.tags,
      },
    },
  );
  expect(put.status()).toBe(200);
  return { course, headers };
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

for (const mobile of [false, true]) {
  test(`review gate, edit, screened abuse and delete (${mobile ? "mobile" : "desktop"})`, async ({
    page,
    request,
  }) => {
    if (mobile) await page.setViewportSize({ width: 390, height: 844 });
    const { course } = await listed(request, {
      title: "Review course",
      description: "Short lessons",
      category: "tech",
      language: "en",
      tags: [],
    });
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
    await expect(
      page.getByRole("heading", { name: "Reviews", exact: true }),
    ).toBeVisible();
    await page.getByRole("button", { name: /^Enroll/ }).click();
    const headers = { Authorization: auth };
    const en = await request.post(
      `${apiBase}/v1/marketplace/courses/${course}/enroll`,
      { headers, data: {} },
    );
    const copy = (await en.json()).course_id;
    const getLessons = async () =>
      (
        await (
          await request.get(`${apiBase}/v1/courses/${copy}`, { headers })
        ).json()
      ).units.flatMap((u: { lessons: { id: string }[] }) => u.lessons);
    const pass = async (id: string) => {
      const started = await request.get(`${apiBase}/v1/lessons/${id}/test`, {
        headers,
      });
      expect(started.status()).toBe(200);
      for (const q of (await started.json()).questions) {
        const graded = await request.post(
          `${apiBase}/v1/lessons/${id}/test/answer`,
          { headers, data: { question_id: q.question_id, choice_index: 0 } },
        );
        expect(graded.status()).toBe(200);
      }
    };
    const lessons = await getLessons();
    await pass(lessons[0].id);
    const early = await request.put(
      `${apiBase}/v1/marketplace/courses/${course}/review`,
      { headers, data: { stars: 5, text: "Helpful" } },
    );
    expect(early.status()).toBe(422);
    expect((await early.json()).lessons_needed).toBe(2);
    await page.reload();
    await expect(page.getByTestId("review-gate")).toContainText("2 more");
    await pass(lessons[1].id);
    await pass(lessons[2].id);
    await page.reload();
    await page.getByLabel("5 stars", { exact: true }).check();
    await page.getByLabel("Review text").fill("Helpful and clear");
    await page
      .getByRole("button", { name: "Post review", exact: true })
      .click();
    await expect(page.getByTestId("review-status")).toContainText(
      "Review saved",
    );
    await expect(page.getByTestId("course-reviews")).toContainText(
      "Helpful and clear",
    );
    await page.getByLabel("4 stars", { exact: true }).check();
    await page.getByLabel("Review text").fill("Great pace, needs examples");
    await page
      .getByRole("button", { name: "Save review", exact: true })
      .click();
    await expect(page.getByTestId("course-reviews")).toContainText(
      "Great pace, needs examples",
    );
    await page
      .getByLabel("Review text")
      .fill("You are worthless abusive-review");
    await page
      .getByRole("button", { name: "Save review", exact: true })
      .click();
    await expect(page.getByTestId("review-status")).toContainText(
      "text is hidden",
    );
    await expect(page.getByTestId("course-reviews")).not.toContainText(
      "worthless",
    );
    await page
      .getByRole("button", { name: "Delete review", exact: true })
      .click();
    await page
      .getByRole("button", { name: "Confirm delete", exact: true })
      .click();
    await expect(page.getByTestId("course-reviews")).toContainText(
      "No reviews yet",
    );
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth - window.innerWidth,
      ),
    ).toBeLessThanOrEqual(0);
  });
}
