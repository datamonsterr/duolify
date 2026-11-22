import {
  answerLessonTest,
  learnerWithCourse,
  openLessonTest,
  playFirstLesson,
  requireLessonGeneration,
} from "./lesson";
import { expect, test } from "./stack";

test.use({ flowId: "F5" });

test(
  "F5 lesson test: wrong answer, retry variant, pass",
  {
    annotation: {
      type: "flow",
      description: "F5 Lesson test fail, retry variant, then pass",
    },
  },
  async ({ page, request, flow }) => {
    await requireLessonGeneration(request);
    await learnerWithCourse(page, "f5");
    await playFirstLesson(page, flow);
    await openLessonTest(page, flow);
    const { missed, sawVariant } = await answerLessonTest(page, flow, {
      missFirst: true,
    });
    expect(missed, "at least one answer was wrong").toBe(true);
    // A missed idea comes back later as a fresh-wording variant of the same idea.
    expect(sawVariant, "the missed idea returned as a retry variant").toBe(
      true,
    );
    await flow.step("lesson test passed after retry");
    await page.getByRole("link", { name: /Back to your path/i }).click();
    await expect(page).toHaveURL(/\/course\/[^/]+$/);
    await flow.step("back on course path");
  },
);
