import { briefToPreviewQuote } from "./course";
import { expect, grantCredits, signUp, test, uniqueEmail } from "./stack";

test.use({ flowId: "F9" });

test(
  "F9 credits: first check-in, wallet, quote before a paid action, purchase",
  {
    annotation: {
      type: "flow",
      description:
        "F9 Credits: first daily check-in, wallet, quote before a paid action, purchase",
    },
  },
  async ({ page, flow }) => {
    const learner = await signUp(
      page,
      uniqueEmail("f9"),
      undefined,
      "/credits",
    );
    // First open of the day: the daily check-in grants Credits once (ADR 0020).
    await expect(
      page
        .getByRole("status")
        .filter({ hasText: /^\+\d[\d,]* Credits · daily check-in/ }),
    ).toBeVisible();
    await page.reload();
    await expect(
      page.getByRole("meter", { name: "Daily free credits" }),
    ).toBeVisible();
    await expect(page.getByText(/^Check-in Credits: \d[\d,]*$/)).toBeVisible();
    await expect(page.getByText("+500")).toHaveCount(0);
    await flow.step("wallet of a new learner after the first check-in");

    // A course preview is a paid action: its quote is shown before anything is charged,
    // and the first check-in covers it.
    await page.goto("/courses/create");
    await briefToPreviewQuote(page);
    const confirm = page.getByRole("button", { name: /^Confirm & preview/ });
    await expect(
      page.getByText(/^Up to \d+ credits · Original terms/),
    ).toBeVisible();
    await expect(confirm).toBeEnabled();
    await expect(
      page.getByText("Not enough eligible paid credits. Your brief is kept."),
    ).toHaveCount(0);
    await flow.step("quote shown before a paid action, covered by check-in");

    // A new learner can no longer reach the insufficient-credits state without spending
    // the check-in first; the mocked suite covers that path (pricing and credits specs).
    // Stand-in for a completed purchase (card payments are off outside test builds).
    test.fixme(
      !(await grantCredits(learner, 500)),
      "no grant mechanism on this target: set E2E_STACK_GRANT_COMMAND to fund a learner",
    );
    await page.goto("/credits");
    await expect(page.getByText("+500")).toBeVisible();
    await flow.step("wallet after purchase");
    // The shell's right rail reads the same wallet.
    const rail = page.getByRole("complementary", { name: "Your progress" });
    await expect(
      rail.getByText(
        /^Daily free \d[\d,]* · Check-in \d[\d,]* · Purchased 500$/,
      ),
    ).toBeVisible();
    await expect(rail.getByText(/^\d[\d,]* credits$/)).toBeVisible();
    await expect(rail.getByText("Balance unavailable")).toHaveCount(0);
    await flow.step("right rail shows the funded balance");
  },
);
