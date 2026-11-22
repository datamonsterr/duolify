import {
  answerLessonTest,
  learnerWithCourse,
  openLessonTest,
  playFirstLesson,
  requireLessonGeneration,
} from "./lesson";
import { expect, test } from "./stack";

test.use({ flowId: "F8" });

const xpToday = /^(\d[\d,]*) \/ (\d[\d,]*) XP today$/;

test(
  "F8 XP, daily goal and streak update after completing a lesson",
  {
    annotation: {
      type: "flow",
      description: "F8 Streak, XP and daily goal update after a lesson",
    },
  },
  async ({ page, request, flow }) => {
    await requireLessonGeneration(request);
    await learnerWithCourse(page, "f8");
    const coursePath = page.url();

    await page.goto("/streak");
    await expect(
      page.getByRole("heading", { name: "Your learning progress", level: 1 }),
    ).toBeVisible();
    const goal = page
      .getByRole("region", { name: "Daily goal progress" })
      .first();
    await expect(goal.getByText(xpToday)).toHaveText(/^0 \//);
    await expect(page.getByText("Start your streak")).toBeVisible();
    await flow.step("progress before the first lesson");

    await page.goto(coursePath);
    await playFirstLesson(page, flow);
    await openLessonTest(page, flow);
    await answerLessonTest(page, flow);
    await expect(page.getByText(/^\+\d+ XP$/).first()).toBeVisible();
    await flow.step("XP earned on the completion screen");

    await page.goto("/streak");
    await expect(goal.getByText(xpToday)).not.toHaveText(/^0 \//);
    const streak = page.getByRole("region", { name: "Your streak" });
    await expect(streak.getByText("day streak")).toBeVisible();
    await expect(streak.getByText("Today is done")).toBeVisible();
    await flow.step("XP, daily goal and streak after the lesson");
  },
);
