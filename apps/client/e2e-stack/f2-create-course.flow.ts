import { createCourse, fundLearner, topic } from "./course";
import { expect, signUp, test, uniqueEmail } from "./stack";

test.use({ flowId: "F2" });

test(
  "F2 create a course: topic, preview, outline, confirm",
  {
    annotation: {
      type: "flow",
      description: "F2 Create a course: topic, outline, confirm",
    },
  },
  async ({ page, flow }) => {
    // Enters through the sign-in deep link: the in-app "Create a course" button drops the
    // web session (see F1b), which would otherwise hide every later step.
    const learner = await signUp(
      page,
      uniqueEmail("f2"),
      undefined,
      "/courses/create",
    );
    await fundLearner(learner);
    await createCourse(page, flow);
    await expect(page.getByRole("heading", { name: topic })).toBeVisible();
    await flow.step("course path");
  },
);
