import { test, expect } from "./accounts-fixture";

for (const mobile of [false, true]) {
  test(`completion XP and configured daily goal ${mobile ? "mobile" : "desktop"}`, async ({
    page,
  }) => {
    await page.setViewportSize(
      mobile ? { width: 390, height: 844 } : { width: 1280, height: 900 },
    );
    await page.emulateMedia({
      colorScheme: mobile ? "dark" : "light",
      reducedMotion: "reduce",
    });
    let goal = 23;
    let complete = false;
    let failed = false;
    await page.route("**/v1/features", (r) =>
      r.fulfill({ json: { lesson_test_tracer_enabled: true } }),
    );
    await page.route("**/v1/gamification", async (r) => {
      if (failed)
        return r.fulfill({
          status: 503,
          json: { error: "Progress unavailable. Retry." },
        });
      if (r.request().method() === "PUT")
        goal = r.request().postDataJSON().daily_goal;
      return r.fulfill({
        json: {
          total_xp: complete ? 23 : 0,
          daily_xp: complete ? 23 : 0,
          daily_goal: goal,
          goal_options: [23, 46],
          goal_met: complete && goal <= 23,
          local_date: "2026-11-10",
          time_zone: "America/New_York",
        },
      });
    });
    await page.route("**/v1/gamification/streak**", (r) =>
      r.fulfill({
        json: {
          current_days: 0,
          longest_days: 0,
          today_done: false,
          local_date: "2026-11-10",
          time_zone: "America/New_York",
          as_of: "2026-11-10T12:00:00Z",
          rollover_pending: false,
          month: "2026-11",
          calendar: [],
        },
      }),
    );
    await page.route("**/v1/gamification/lessons/lesson-one", (r) =>
      r.fulfill({ json: { xp: 23, perfect: true } }),
    );
    await page.route("**/v1/lessons/lesson-one/test", (r) =>
      r.fulfill({
        json: {
          course_id: "course-one",
          passed: complete,
          questions: complete
            ? []
            : [
                {
                  question_id: "q-one",
                  topic_id: "stars",
                  prompt: "What are stars?",
                  choices: ["Distant suns", "Clouds"],
                },
              ],
        },
      }),
    );
    await page.route("**/v1/lessons/lesson-one/test/answer", (r) => {
      complete = true;
      return r.fulfill({
        json: {
          correct: true,
          passed: true,
          answer_index: 0,
          resolved_topics: 1,
          total_topics: 1,
          retry_queue_count: 0,
        },
      });
    });

    await page.goto("/streak");
    await expect(
      page.getByRole("progressbar", { name: "Daily XP goal" }),
    ).toHaveAttribute("aria-valuenow", "0");
    await page
      .getByRole("combobox", { name: "Daily XP goal" })
      .selectOption("46");
    await page.getByRole("button", { name: "Save goal" }).click();
    await expect(page.getByText("0 / 46 XP today")).toBeVisible();
    await page
      .getByRole("combobox", { name: "Daily XP goal" })
      .selectOption("23");
    await page.getByRole("button", { name: "Save goal" }).click();
    await page.goto("/lesson/lesson-one/test");
    await page.getByLabel("Distant suns").check();
    await page.getByRole("button", { name: "Check", exact: true }).click();
    await page.getByRole("button", { name: "Continue", exact: true }).click();
    await expect(page.getByText("+23 XP", { exact: true })).toBeVisible();
    await expect(
      page.getByText("Daily goal met", { exact: true }),
    ).toBeVisible();
    await expect(page.getByText("23 / 23 XP today")).toBeVisible();
    await page.reload();
    await expect(
      page.getByText("Review complete", { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByText("XP was already earned for this lesson."),
    ).toBeVisible();
    await page.goto("/streak");
    await expect(page.getByText("23 / 23 XP today")).toBeVisible();
    failed = true;
    await page.reload();
    await expect(page.getByRole("alert")).toContainText("Progress unavailable");
    failed = false;
    await page.getByRole("button", { name: "Retry progress" }).click();
    await expect(page.getByText("23 / 23 XP today")).toBeVisible();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);
  });
}
