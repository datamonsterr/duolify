// Lesson steps shared by F3 and F4.
import type { APIRequestContext, Page } from "@playwright/test";
import { createCourse, fundLearner } from "./course";
import {
  expect,
  features,
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
  const learner = await signUp(
    page,
    uniqueEmail(flowId),
    undefined,
    "/courses/create",
  );
  await fundLearner(learner);
  await createCourse(page);
}

/** Prepare (quoted) and open the first lesson, then play every slide to the end. */
export async function playFirstLesson(page: Page, flow: Flow): Promise<void> {
  const prepare = page.getByRole("button", { name: /Prepare lesson/i }).first();
  await expect(prepare).toBeVisible();
  await flow.step("course path, first lesson ready to prepare");
  await prepare.click();
  await page.getByRole("button", { name: "Accept quote terms" }).click();
  const start = page.getByRole("link", { name: /Start lesson/i }).first();
  await expect(start).toBeVisible({ timeout: 180_000 });
  await flow.step("first lesson generated");
  await start.click();
  await expect(page).toHaveURL(/\/lesson\/[^/]+$/);
  await expect(page.getByText(/Slide 1 of \d+/)).toBeVisible();
  await flow.step("lesson player, first slide");
  const next = page.getByRole("button", { name: "Next" });
  while (await next.isEnabled().catch(() => false)) {
    await next.click();
  }
  await expect(page.getByText(/Slide (\d+) of \1/)).toBeVisible();
  await flow.step("last slide");
}
