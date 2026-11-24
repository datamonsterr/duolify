import { expect, signUp, skipOnboarding, test, uniqueEmail } from "./stack";

test.use({ flowId: "F1b" });

test(
  "F1b new learner home and in-app navigation to course creation keep the learner signed in",
  {
    annotation: {
      type: "flow",
      description:
        "F1b New learner home; Courses to Create a course keeps the web session",
    },
  },
  async ({ page, flow }) => {
    await signUp(page, uniqueEmail("f1b"), undefined, undefined, {
      keepOnboarding: true,
    });
    await expect(
      page
        .getByRole("status")
        .filter({ hasText: /^\+\d[\d,]* Credits · daily check-in/ }),
    ).toBeVisible();
    await flow.step("first daily check-in toast");
    await skipOnboarding(page);
    await page.getByRole("link", { name: "Learn" }).first().click();
    // A learner with no courses gets the empty Courses state, not a fixture course.
    await expect(
      page.getByRole("heading", {
        name: "What would you like to keep learning?",
      }),
    ).toBeVisible();
    await expect(page.getByText("Could not load this course.")).toHaveCount(0);
    // Sidebar panels read learner data with the session token.
    await expect(page.getByText("Code is invalid or expired.")).toHaveCount(0);
    await flow.step("learn home for a new learner");

    const creation = page.getByRole("heading", {
      name: "Create a course",
      level: 1,
    });
    await page.getByRole("link", { name: "Create a course" }).first().click();
    await expect(creation).toBeVisible();
    await flow.step("create course from the empty state");

    await page.getByRole("link", { name: "Cancel and keep draft" }).click();
    await expect(page.getByRole("heading", { name: "Courses" })).toBeVisible();
    await flow.step("courses");
    await page.getByRole("link", { name: "Create a course" }).first().click();
    await expect(creation).toBeVisible();
    await flow.step("create course page, still signed in");
  },
);
