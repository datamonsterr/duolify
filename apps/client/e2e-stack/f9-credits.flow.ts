import { briefToPreviewQuote, fundLearner } from "./course";
import { expect, signUp, test, uniqueEmail } from "./stack";

test.use({ flowId: "F9" });

test(
  "F9 credits: wallet, quote before a paid action, insufficient then funded",
  {
    annotation: {
      type: "flow",
      description:
        "F9 Credits: wallet, quote before a paid action, insufficient-credits path",
    },
  },
  async ({ page, flow }) => {
    const learner = await signUp(
      page,
      uniqueEmail("f9"),
      undefined,
      "/credits",
    );
    await page.goto("/credits");
    await expect(
      page.getByRole("heading", { name: "Credits", level: 1 }),
    ).toBeVisible();
    // A new learner has daily free credits and nothing purchased.
    await expect(
      page.getByRole("meter", { name: "Daily free credits" }),
    ).toBeVisible();
    await expect(page.getByText("Daily free credits granted")).toBeVisible();
    await expect(page.getByText("+500")).toHaveCount(0);
    await flow.step("wallet of a new learner");

    // A course preview is a paid action: its quote is shown before anything is charged,
    // and daily free credits cannot pay for it.
    await page.goto("/courses/create");
    await briefToPreviewQuote(page);
    const confirm = page.getByRole("button", { name: /^Confirm & preview/ });
    await expect(
      page.getByText(/^Up to \d+ credits · Original terms/),
    ).toBeVisible();
    await expect(
      page.getByRole("alert").filter({
        hasText: "Not enough eligible paid credits. Your brief is kept.",
      }),
    ).toBeVisible();
    await expect(confirm).toBeDisabled();
    await flow.step("quote shown, not enough paid credits");

    await page
      .getByRole("link", { name: "Get credits — your brief is kept" })
      .click();
    await expect(page).toHaveURL(/\/credits/);
    await flow.step("credit options");

    // Stand-in for a completed purchase (card payments are off outside test builds).
    await fundLearner(learner);
    await page.reload();
    await expect(page.getByText("+500")).toBeVisible();
    await flow.step("wallet after purchase");
    // The shell's right rail reads the same wallet: purchased 500 on top of daily free.
    const rail = page.getByRole("complementary", { name: "Your progress" });
    await expect(
      rail.getByText(/^Daily free \d[\d,]* · Purchased 500$/),
    ).toBeVisible();
    await expect(rail.getByText(/^\d[\d,]* credits$/)).toBeVisible();
    await expect(rail.getByText("Balance unavailable")).toHaveCount(0);
    await flow.step("right rail shows the funded balance");

    await page.goto("/courses/create");
    await expect(
      page.getByRole("heading", { name: "Create a course", level: 1 }),
    ).toBeVisible();
    // The confirmed brief is kept; review a fresh quote with the new balance.
    const review = page
      .getByRole("button", { name: "Review preview cost" })
      .or(page.getByRole("button", { name: "Renew preview quote" }));
    if (!(await confirm.isVisible())) {
      if (await review.first().isVisible()) await review.first().click();
      else await briefToPreviewQuote(page);
    }
    await expect(confirm).toBeEnabled();
    await expect(
      page.getByText("Not enough eligible paid credits. Your brief is kept."),
    ).toHaveCount(0);
    await flow.step("quote affordable after purchase");
  },
);
