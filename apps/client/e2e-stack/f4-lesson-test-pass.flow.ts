import {
  learnerWithCourse,
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
    await page
      .getByRole("button", { name: /Take lesson test/i })
      .or(page.getByRole("link", { name: /Take lesson test/i }))
      .click();
    await expect(page).toHaveURL(/\/lesson\/[^/]+\/test$/);
    await flow.step("lesson test start");

    // Answer each question; a wrong pick shows the correction, then the next choice is tried.
    const done = page.getByText(/Lesson complete!/i);
    for (
      let question = 0;
      question < 20 && !(await done.isVisible());
      question++
    ) {
      const choices = page.getByRole("radio");
      const count = await choices.count();
      for (let i = 0; i < count; i++) {
        await choices.nth(i).check();
        await page.getByRole("button", { name: /Check/i }).click();
        const correct = await page
          .getByText(/Correct/i)
          .first()
          .isVisible();
        if (question === 0 && i === 0) await flow.step("first answer checked");
        await page.getByRole("button", { name: /Continue/i }).click();
        if (correct) break;
      }
    }
    await expect(done).toBeVisible();
    await flow.step("lesson test passed");
    await page.getByRole("link", { name: /Back to your path/i }).click();
    await expect(page).toHaveURL(/\/course\/[^/]+$/);
    await flow.step("back on course path");
  },
);
