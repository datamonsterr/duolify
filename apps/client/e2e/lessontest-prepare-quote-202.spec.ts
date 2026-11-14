import { test, expect } from "./accounts-fixture";
import type { Page } from "@playwright/test";

const courseId = "c1111111-1111-4111-8111-111111111111";
const lessonId = "l1111111-1111-4111-8111-111111111111";
const genId = "gen-202";

async function mockPrivateCourse(page: Page) {
  const calls = { quote: 0, accept: 0, prepare: [] as number[] };
  let lessonState = "ready_to_generate";
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
          {
            id: lessonId,
            title: "A map of the night",
            goal: "Sky coordinates",
            state: lessonState,
          },
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
  await page.route("**/v1/quotes", (r) => {
    calls.quote++;
    accepted = false;
    return r.fulfill({
      status: 201,
      json: {
        id: "quote-202",
        action: "lesson_content",
        pricing_version: "usd-quotes-2026-11-09",
        currency: "USD",
        content_ceiling: 137,
        audio_ceiling: 0,
        daily_free_eligible: false,
        expires_at: new Date(Date.now() + 600000).toISOString(),
        accepted: false,
      },
    });
  });
  await page.route("**/v1/quotes/quote-202/accept", (r) => {
    calls.accept++;
    accepted = true;
    return r.fulfill({ json: { accepted: true } });
  });
  await page.route(`**/v1/lessons/${lessonId}/prepare`, (r) => {
    const status = accepted ? 202 : 409;
    calls.prepare.push(status);
    if (accepted) lessonState = "generating";
    return r.fulfill({
      status,
      json: accepted ? { id: genId, state: "generating" } : { error: "quote" },
    });
  });
  await page.route(`**/v1/lesson-generations/${genId}/events`, (r) => {
    lessonState = "current";
    return r.fulfill({
      status: 200,
      headers: { "Content-Type": "text/event-stream" },
      body: 'event: stage\ndata: {"state":"completed","stage":"Ready"}\n\n',
    });
  });
  return calls;
}

test("private course: quote is shown, accepted on confirm, then prepare succeeds", async ({
  page,
}) => {
  const calls = await mockPrivateCourse(page);
  await page.goto(`/course/${courseId}`);
  await page.getByRole("button", { name: /Prepare lesson/i }).click();

  const dialog = page.getByRole("dialog");
  await expect(dialog.getByText("Content ceiling: 137 credits")).toBeVisible();
  expect(calls.accept).toBe(0);
  expect(calls.prepare).toEqual([]);

  await dialog.getByRole("button", { name: "Accept quote terms" }).click();
  await expect(page.getByRole("link", { name: /Start lesson/i })).toBeVisible();
  expect(calls.accept).toBe(1);
  expect(calls.prepare).toEqual([202]);
  await expect(page.getByRole("alert")).toHaveCount(0);
});

test("private course: declining the quote leaves the lesson untouched", async ({
  page,
}) => {
  const calls = await mockPrivateCourse(page);
  await page.goto(`/course/${courseId}`);
  await page.getByRole("button", { name: /Prepare lesson/i }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByText("Content ceiling: 137 credits")).toBeVisible();
  await dialog.getByRole("button", { name: "Not now" }).click();

  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: /Prepare lesson/i }),
  ).toBeEnabled();
  expect(calls.accept).toBe(0);
  expect(calls.prepare).toEqual([]);
});
