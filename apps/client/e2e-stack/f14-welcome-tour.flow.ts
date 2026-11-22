import { expect, signUp, test, uniqueEmail } from "./stack";

test.use({ flowId: "F14" });

test(
  "F14 first-run welcome and guided tour, every step",
  {
    annotation: {
      type: "flow",
      description: "F14 First-run welcome and guided tour, every step",
    },
  },
  async ({ page, flow }) => {
    // No deep link: a brand-new learner lands on Learn behind the welcome.
    await signUp(page, uniqueEmail("f14"), undefined, undefined, {
      keepOnboarding: true,
    });
    const welcome = page.getByTestId("onboarding-welcome");
    await expect(welcome).toBeVisible();
    await flow.step("welcome");

    await page
      .getByRole("button", { name: "Skip for now", exact: true })
      .click();
    const tour = page.getByTestId("onboarding-tour");
    await expect(tour).toBeVisible();
    const total = Number(await tour.getAttribute("data-step-total"));
    expect(total).toBeGreaterThan(0);
    for (let i = 1; i <= total; i++) {
      await expect(tour).toHaveAttribute("data-step-index", String(i));
      await expect(
        tour.getByText(`Step ${i} of ${total}`, { exact: true }),
      ).toBeVisible();
      await expect(
        page.locator('[data-onboarding-active="true"]'),
      ).toBeVisible();
      await flow.step(`tour step ${i} of ${total}`);
      await tour
        .getByRole("button", {
          name: i === total ? "Done" : "Next",
          exact: true,
        })
        .click();
    }
    await expect(tour).toHaveCount(0);
    await expect(welcome).toHaveCount(0);
    await flow.step("tour done");

    // Progress is stored on the server: a reload shows neither again.
    await page.reload();
    await expect(page.locator("#main-content h1")).toBeVisible();
    await expect(welcome).toHaveCount(0);
    await expect(tour).toHaveCount(0);
    await flow.step("after reload, no welcome or tour");
  },
);
