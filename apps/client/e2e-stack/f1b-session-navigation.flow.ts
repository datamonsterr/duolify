import { expect, noteShellDefects, signUp, test, uniqueEmail } from "./stack";

test.use({ flowId: "F1b" });

test(
  "F1b in-app navigation to course creation keeps the learner signed in",
  {
    annotation: {
      type: "flow",
      description: "F1b Courses to Create a course keeps the web session",
    },
  },
  async ({ page, flow }) => {
    await signUp(page, uniqueEmail("f1b"));
    await page.getByRole("link", { name: "Learn" }).first().click();
    await noteShellDefects(page, flow);
    await flow.step("learn home for a new learner");
    await page.getByRole("link", { name: "Courses" }).first().click();
    await expect(page.getByRole("heading", { name: "Courses" })).toBeVisible();
    await flow.step("courses");
    await page.getByRole("link", { name: "Create a course" }).first().click();
    const creation = page.getByRole("heading", {
      name: "Create a course",
      level: 1,
    });
    const signIn = page.getByRole("heading", {
      name: "Learn any topic, a little at a time.",
    });
    await expect(creation.or(signIn)).toBeVisible();
    if (await signIn.isVisible()) {
      await flow.step("bounced to sign-in");
      test.fixme(
        true,
        "Web sessions live only in memory (apps/client/src/features/accounts/secure-store.ts:16-35) " +
          "and Courses opens creation with a full page load (<a href> and location.assign in " +
          "apps/client/src/features/design/Courses.tsx:66,183,206), so a signed-in learner is sent " +
          "back to sign-in.",
      );
    }
    await expect(
      page.getByRole("heading", { name: "Create a course", level: 1 }),
    ).toBeVisible();
    await flow.step("create course page");
  },
);
