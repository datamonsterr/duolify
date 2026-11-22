import {
  learnerWithCourse,
  playFirstLesson,
  requireLessonGeneration,
} from "./lesson";
import { expect, test } from "./stack";

test.use({ flowId: "F6" });

const balance = /Daily free (\d[\d,]*) · Purchased (\d[\d,]*)/;
const amount = (text: string | null, group: 1 | 2) =>
  Number((text?.match(balance)?.[group] ?? "NaN").replace(/,/g, ""));

test(
  "F6 tutor chat from a paused lesson, paid from daily free credits",
  {
    annotation: {
      type: "flow",
      description: "F6 Tutor chat from a paused lesson (daily free credits)",
    },
  },
  async ({ page, request, flow }) => {
    await requireLessonGeneration(request);
    await learnerWithCourse(page, "f6");
    await playFirstLesson(page, flow);

    await page.getByRole("button", { name: "Ask tutor", exact: true }).click();
    await expect(
      page.getByRole("status").filter({ hasText: "Lesson paused" }),
    ).toBeVisible();
    const tutor = page.getByRole("complementary", { name: "Tutor chat" });
    await expect(tutor.getByRole("log", { name: "Tutor" })).toBeVisible();
    await flow.step("tutor open, lesson paused");

    await tutor
      .getByRole("textbox", { name: "Message" })
      .fill("What is the main idea of this lesson, in one sentence?");
    await expect(tutor.getByText(/up to \d[\d,]* credits/i)).toBeVisible();
    const funds = tutor.getByText(balance);
    await expect(funds).toBeVisible();
    const dailyBefore = amount(await funds.textContent(), 1);
    const paidBefore = amount(await funds.textContent(), 2);
    expect(dailyBefore).toBeGreaterThan(0);
    await flow.step("question with its estimate");

    await tutor.getByRole("button", { name: "Send", exact: true }).click();
    const replies = tutor.locator(".tutor-msg.ai");
    await expect(replies).toHaveCount(1);
    await expect(tutor.getByRole("button", { name: "Stop reply" })).toHaveCount(
      0,
    );
    await expect(tutor.getByRole("alert")).toHaveCount(0);
    await expect(replies.first()).not.toBeEmpty();
    await flow.step("tutor replied");

    // Daily free credits pay first; purchased credits are untouched.
    await expect
      .poll(async () => amount(await funds.textContent(), 1))
      .toBeLessThan(dailyBefore);
    expect(amount(await funds.textContent(), 2)).toBe(paidBefore);
    await flow.step("daily free credits spent");

    await page.getByRole("button", { name: "Close Ask tutor" }).click();
    await expect(page.getByRole("button", { name: "Next" })).toBeVisible();
    await flow.step("back to the lesson");
  },
);
