import {
  answerLessonTest,
  learnerWithCourse,
  openLessonTest,
  playFirstLesson,
  requireLessonGeneration,
} from "./lesson";
import { expect, test } from "./stack";

test.use({ flowId: "F4" });

test(
  "F4 lesson test pass path",
  {
    annotation: { type: "flow", description: "F4 Lesson test pass path" },
  },
  async ({ page, request, flow }) => {
    await requireLessonGeneration(request);
    await learnerWithCourse(page, "f4");
    await playFirstLesson(page, flow);
    await openLessonTest(page, flow);
    await answerLessonTest(page, flow);
    await flow.step("lesson test passed");
    await page.getByRole("link", { name: /Back to your path/i }).click();
    await expect(page).toHaveURL(/\/course\/[^/]+$/);
    await flow.step("back on course path");
  },
);
