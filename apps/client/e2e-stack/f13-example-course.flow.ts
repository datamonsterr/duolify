import { answerLessonTest, openLessonTest, playOpenedLesson } from "./lesson";
import { expect, signUp, test, uniqueEmail } from "./stack";

test.use({ flowId: "F13" });

const example = "Product Management Foundations";
const xpToday = /^(\d[\d,]*) \/ (\d[\d,]*) XP today$/;

test(
  "F13 a new learner with no purchased credits enrolls in an example course and passes lesson 1",
  {
    annotation: {
      type: "flow",
      description:
        "F13 Free example course: enroll, play lesson 1, pass its test, see progress",
    },
  },
  async ({ page, flow }) => {
    // Example courses are authored and free: no AI call, no credits, no tracer flag.
    await signUp(page, uniqueEmail("f13"), undefined, "/courses");
    await page.goto("/courses");
    const examples = page.getByRole("region", {
      name: "Start with an example",
    });
    await expect(examples).toBeVisible();
    await expect(examples.getByRole("article")).toHaveCount(2);
    await flow.step("example courses offered");

    await examples.getByRole("button", { name: `Start ${example}` }).click();
    await expect(page).toHaveURL(/\/course\/[0-9a-f-]{36}$/);
    const coursePath = page.url();
    await expect(page.getByRole("heading", { name: example })).toBeVisible();
    await expect(page.getByTestId("example-label")).toHaveText(
      "Example course · Free",
    );
    await flow.step("example course path");

    const first = page.getByRole("link", { name: /^Start lesson: / }).first();
    const title = (await first.getAttribute("aria-label"))!.replace(
      /^Start lesson: /,
      "",
    );
    await first.click();
    await playOpenedLesson(page, flow);
    await openLessonTest(page, flow);
    await answerLessonTest(page, flow);
    await expect(page.getByText(/^\+\d+ XP$/).first()).toBeVisible();
    await flow.step("lesson test passed, XP earned");

    await page.goto(coursePath);
    await expect(page.getByRole("heading", { name: example })).toBeVisible();
    await expect(
      page.getByRole("link", { name: `Start lesson: ${title}` }),
    ).toHaveCount(0);
    await expect(page.getByText("Completed · Review").first()).toBeVisible();
    await flow.step("lesson 1 completed on the path");

    await page.goto("/streak");
    await expect(
      page
        .getByRole("region", { name: "Daily goal progress" })
        .first()
        .getByText(xpToday),
    ).not.toHaveText(/^0 \//);
    await expect(
      page
        .getByRole("region", { name: "Your streak" })
        .getByText("Today is done"),
    ).toBeVisible();
    await flow.step("XP and streak after the example lesson");
  },
);
