import { expect, signUp, test, uniqueEmail } from "./stack";

test.use({ flowId: "F1d" });

const toast = /^\+(\d[\d,]*) Credits · daily check-in/;

test(
  "F1d daily check-in credits on the first open of the day, once",
  {
    annotation: {
      type: "flow",
      description: "F1d Daily check-in credits toast, once per day",
    },
  },
  async ({ page, flow }) => {
    await signUp(page, uniqueEmail("f1d"), undefined, "/credits");
    const notice = page.getByRole("status").filter({ hasText: toast });
    await expect(notice).toBeVisible();
    // ADR 0020: the first check-in adds the first-time bonus to the daily amount.
    const granted = Number(
      ((await notice.textContent())?.match(toast)?.[1] ?? "0").replace(
        /,/g,
        "",
      ),
    );
    expect(granted).toBeGreaterThan(0);
    await flow.step("first check-in toast");

    await page.reload();
    await expect(
      page.getByRole("heading", { name: "Credits", level: 1 }),
    ).toBeVisible();
    await expect(page.getByText(/^Check-in Credits: \d[\d,]*$/)).toBeVisible();
    // A second open the same day grants nothing and shows no toast.
    await page.waitForLoadState("networkidle").catch(() => {});
    await expect(notice).toHaveCount(0);
    await flow.step("same day: no second check-in");
  },
);
