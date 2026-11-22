import {
  learnerWithCourse,
  playFirstLesson,
  requireLessonGeneration,
} from "./lesson";
import { expect, test } from "./stack";

test.use({ flowId: "F3" });

test(
  "F3 open and play the first lesson to the end",
  {
    annotation: {
      type: "flow",
      description: "F3 Open and play the first lesson to the end",
    },
  },
  async ({ page, request, flow }) => {
    await requireLessonGeneration(request);
    await learnerWithCourse(page, "f3");
    await playFirstLesson(page, flow);
    await expect(
      page
        .getByRole("button", { name: /Finish lesson|Take lesson test/i })
        .or(page.getByRole("link", { name: /Take lesson test/i })),
    ).toBeVisible();
    await flow.step("lesson end");
  },
);
