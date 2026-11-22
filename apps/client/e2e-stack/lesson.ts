// Lesson steps shared by F3 and F4.
import type { APIRequestContext, Page } from "@playwright/test";
import { awaitFirstCheckIn, createCourse } from "./course";
import {
  expect,
  features,
  live,
  signUp,
  test,
  uniqueEmail,
  type Flow,
} from "./stack";

/** Lesson generation and the lesson test only exist behind the tracer flag. */
export async function requireLessonGeneration(
  request: APIRequestContext,
): Promise<void> {
  const enabled = (await features(request)).lesson_test_tracer_enabled === true;
  test.fixme(
    !enabled,
    "Lesson generation and the lesson test are gated by LESSON_TEST_TRACER_ENABLED, which is off " +
      "in infra/deploy/compose.yml and by default (services/api/internal/config/config.go:79). " +
      "With it off the worker never starts the lesson queue (services/api/cmd/api/main.go:204) and " +
      "the API 404s /v1/lesson-generations/* and /v1/lessons/{id}[/prepare|/test] " +
      "(services/api/internal/httpapi/router.go:520). GET /v1/features reports " +
      "lesson_test_tracer_enabled=false.",
  );
}

/** New learner with a created course, on the course path page. */
export async function learnerWithCourse(page: Page, flowId: string) {
  await signUp(page, uniqueEmail(flowId), undefined, "/courses/create");
  await awaitFirstCheckIn(page);
  await createCourse(page);
}

/** Prepare (quoted) and open the first lesson, then play every slide to the end. */
export async function playFirstLesson(page: Page, flow: Flow): Promise<void> {
  const prepare = page.getByRole("button", { name: /Prepare lesson/i }).first();
  await expect(prepare).toBeVisible();
  await flow.step("course path, first lesson ready to prepare");
  await prepare.click();
  await page.getByRole("button", { name: "Accept quote terms" }).click();
  await flow.step("lesson quote accepted, preparing");
  const start = page.getByRole("link", { name: /Start lesson/i }).first();
  const failed = page.getByRole("alert").filter({
    hasText: "Something went wrong. Try again in a moment.",
  });
  await expect(start.or(failed)).toBeVisible({ timeout: 180_000 });
  if (await failed.isVisible()) {
    await flow.step("lesson generation failed");
    throw new Error(
      `Lesson generation failed (${live ? "live" : "mock"} engine): see the api and engine logs`,
    );
  }
  await flow.step("first lesson generated");
  await start.click();
  await playOpenedLesson(page, flow);
}

/** From a click that opens the player: play every slide to the end. */
export async function playOpenedLesson(page: Page, flow: Flow): Promise<void> {
  await expect(page).toHaveURL(/\/lesson\/[^/]+$/);
  await expect(page.getByText(/Slide 1 of \d+/)).toBeVisible();
  await flow.step("lesson player, first slide");
  // The last slide replaces "Next" with the "Take lesson test" link.
  const next = page.getByRole("button", { name: "Next", exact: true });
  const toTest = page.getByRole("link", { name: "Take lesson test" });
  for (let i = 0; i < 100; i++) {
    await expect(next.or(toTest)).toBeVisible();
    if (await toTest.isVisible()) break;
    await next.click();
  }
  await expect(page.getByText(/Slide (\d+) of \1/)).toBeVisible();
  await flow.step("last slide");
}

/** From the player's last slide, open the lesson test. */
export async function openLessonTest(page: Page, flow: Flow): Promise<void> {
  await page
    .getByRole("button", { name: /Take lesson test/i })
    .or(page.getByRole("link", { name: /Take lesson test/i }))
    .click();
  await expect(page).toHaveURL(/\/lesson\/[^/]+\/test$/);
  await expect(
    page.getByRole("group", { name: "Question choices" }),
  ).toBeVisible();
  await flow.step("lesson test start");
}

/**
 * Answer the lesson test until "Lesson complete!". Choices already tried for a question (keyed by
 * its choice texts) are not repeated. With `missFirst`, choices are tried last-first until one
 * answer is wrong, so the retry-variant path is exercised.
 */
export async function answerLessonTest(
  page: Page,
  flow: Flow,
  { missFirst = false } = {},
): Promise<{ missed: boolean; sawVariant: boolean }> {
  const done = page.getByRole("heading", { name: "Lesson complete!" });
  const choices = page
    .getByRole("group", { name: "Question choices" })
    .getByRole("radio");
  const right = page.getByText("Correct!", { exact: true });
  const wrong = page.getByText("Not quite — here's why", { exact: true });
  const variantBadge = page.getByText(
    /Retry variant · Same idea, fresh wording/,
  );
  const tried = new Map<string, Set<number>>();
  let missed = false;
  let sawVariant = false;
  for (let turn = 0; turn < 40; turn++) {
    await expect(done.or(choices.first())).toBeVisible();
    if (await done.isVisible()) break;
    const names = await choices.evaluateAll((els) =>
      els.map((el) => el.getAttribute("aria-label") ?? ""),
    );
    const key = names.join("|");
    if (!sawVariant && (await variantBadge.isVisible())) {
      sawVariant = true;
      await flow.step("retry variant question");
    }
    const used = tried.get(key) ?? new Set<number>();
    tried.set(key, used);
    const order = names.map((_, i) => i);
    if (missFirst && !missed) order.reverse();
    const pick = order.find((i) => !used.has(i)) ?? 0;
    used.add(pick);
    await choices.nth(pick).check();
    await page.getByRole("button", { name: "Check", exact: true }).click();
    await expect(right.or(wrong)).toBeVisible();
    if (await wrong.isVisible()) {
      if (!missed) await flow.step("wrong answer explained");
      missed = true;
    } else if (turn === 0) await flow.step("first answer checked");
    await page.getByRole("button", { name: "Continue", exact: true }).click();
  }
  await expect(done).toBeVisible();
  return { missed, sawVariant };
}
