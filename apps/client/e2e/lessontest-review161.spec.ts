import { test, expect } from "./accounts-fixture";

for (const status of [400, 410, 500]) {
  test(`answer HTTP ${status} stays on the question without offline grading`, async ({
    page,
  }) => {
    await page.route("**/v1/features", (r) =>
      r.fulfill({ json: { lesson_test_tracer_enabled: true } }),
    );
    await page.route("**/v1/lessons/review161/test", (r) =>
      r.fulfill({
        json: {
          session_id: "opaque-session",
          questions: [
            {
              question_id: "opaque-item",
              topic_id: "topic",
              prompt: "Which answer?",
              choices: ["First", "Second"],
            },
          ],
          total_topics: 1,
          resolved_topics: 0,
        },
      }),
    );
    await page.route("**/v1/lessons/review161/test/answer", (r) =>
      r.fulfill({ status, json: { error: "Answer could not be saved" } }),
    );
    await page.goto("/lesson/review161/test");
    await page.getByLabel("First", { exact: true }).click();
    await page.getByRole("button", { name: "Check", exact: true }).click();
    await expect(page.getByText("Answer could not be saved")).toBeVisible();
    await expect(
      page.getByText("Waiting to sync", { exact: true }),
    ).toHaveCount(0);
    await expect(
      page.getByText("Lesson complete!", { exact: true }),
    ).toHaveCount(0);
    expect(
      await page.evaluate(() =>
        localStorage.getItem("lessontest:sync-queue:review161"),
      ),
    ).toBeNull();
  });
}
