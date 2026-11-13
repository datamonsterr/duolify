import { test, expect } from "./accounts-fixture";

type Day = { date: string; state: string };

function november(overrides: Record<string, string>): Day[] {
  return Array.from({ length: 30 }, (_, i) => {
    const date = `2026-11-${String(i + 1).padStart(2, "0")}`;
    return { date, state: overrides[date] ?? (i + 1 > 9 ? "future" : "none") };
  });
}

const active = {
  current_days: 7,
  longest_days: 23,
  today_done: false,
  local_date: "2026-11-09",
  time_zone: "Asia/Ho_Chi_Minh",
  as_of: "2026-11-09T08:42:00Z",
  rollover_pending: false,
  month: "2026-11",
  calendar: november({
    "2026-11-03": "done",
    "2026-11-04": "done",
    "2026-11-05": "missed",
    "2026-11-06": "done",
    "2026-11-07": "done",
    "2026-11-08": "done",
    "2026-11-09": "today",
  }),
};

for (const mobile of [false, true]) {
  test(`learner-local streak days ${mobile ? "mobile" : "desktop"}`, async ({
    page,
  }) => {
    await page.setViewportSize(
      mobile ? { width: 390, height: 844 } : { width: 1280, height: 900 },
    );
    await page.emulateMedia({
      colorScheme: mobile ? "dark" : "light",
      reducedMotion: "reduce",
    });
    let body: Record<string, unknown> = active;
    let failed = false;
    let delay = 0;
    const months: string[] = [];
    await page.route("**/v1/features", (r) =>
      r.fulfill({ json: { lesson_test_tracer_enabled: true } }),
    );
    await page.route("**/v1/gamification", (r) =>
      r.fulfill({
        json: {
          total_xp: 0,
          daily_xp: 0,
          daily_goal: 20,
          goal_options: [10, 20],
          goal_met: false,
          local_date: "2026-11-09",
          time_zone: "Asia/Ho_Chi_Minh",
        },
      }),
    );
    await page.route("**/v1/gamification/streak**", async (r) => {
      months.push(new URL(r.request().url()).searchParams.get("month") ?? "");
      if (delay) await new Promise((x) => setTimeout(x, delay));
      if (failed)
        return r.fulfill({
          status: 503,
          json: { error: "Streak unavailable. Retry." },
        });
      return r.fulfill({ json: body });
    });

    delay = 400;
    await page.goto("/streak");
    await expect(page.getByText("Loading streak…")).toBeVisible();
    delay = 0;
    const streak = page.getByRole("region", { name: "Your streak" });
    await expect(streak.getByText("7", { exact: true })).toBeVisible();
    await expect(streak.getByText("day streak")).toBeVisible();
    await expect(streak.getByText("Longest streak: 23 days")).toBeVisible();
    await expect(streak.getByText("Time zone: Asia/Ho_Chi_Minh")).toBeVisible();
    await expect(streak.getByText("Today isn't done yet")).toBeVisible();
    await expect(
      streak.getByText("Finish one lesson before the day ends to make it 8."),
    ).toBeVisible();
    await expect(
      streak.getByRole("link", { name: "Start a lesson" }),
    ).toBeVisible();
    // Marks carry text labels, not only color.
    await expect(streak.getByRole("cell", { name: "3 Done" })).toBeVisible();
    await expect(streak.getByRole("cell", { name: "5 Missed" })).toBeVisible();
    await expect(streak.getByRole("cell", { name: "9 Today" })).toBeVisible();

    // List alternative is keyboard operable and keeps the same facts.
    const toggle = streak.getByRole("button", { name: "Show as list" });
    await toggle.focus();
    await page.keyboard.press("Enter");
    await expect(
      streak.getByRole("listitem").filter({ hasText: "Nov 5, 2026: Missed" }),
    ).toBeVisible();
    await streak.getByRole("button", { name: "Show as calendar" }).click();

    // Month navigation asks the server for that month.
    await streak.getByRole("button", { name: "Previous month" }).click();
    await expect.poll(() => months.at(-1)).toBe("2026-10");

    // Completing across local midnight extends once, server-decided.
    body = {
      ...active,
      current_days: 8,
      longest_days: 23,
      today_done: true,
      calendar: november({
        "2026-11-08": "done",
        "2026-11-09": "done",
        "2026-11-10": "today",
      }),
      local_date: "2026-11-10",
    };
    await page.reload();
    await expect(streak.getByText("8", { exact: true })).toBeVisible();
    await expect(streak.getByText("Today is done")).toBeVisible();

    // Delayed rollover shows an as-of state instead of a guessed answer.
    body = { ...active, rollover_pending: true, today_done: false };
    await page.reload();
    await expect(streak.getByText("Rollover in progress")).toBeVisible();
    await expect(
      streak.getByText("Updating your streak for the new day…"),
    ).toBeVisible();
    await expect(streak.getByText(/Streak as of/)).toBeVisible();

    // Missed day resets the streak while keeping the longest one.
    body = { ...active, current_days: 0, longest_days: 23 };
    await page.reload();
    await expect(streak.getByText("Welcome back")).toBeVisible();
    await expect(
      streak.getByText(
        "Start with one small lesson. Your longest streak (23 days) is still on record.",
      ),
    ).toBeVisible();

    // Empty.
    body = {
      ...active,
      current_days: 0,
      longest_days: 0,
      calendar: november({ "2026-11-09": "today" }),
    };
    await page.reload();
    await expect(streak.getByText("Start your streak")).toBeVisible();

    // Error with recovery.
    failed = true;
    await page.reload();
    await expect(page.getByRole("alert")).toContainText("Streak unavailable");
    failed = false;
    body = active;
    await page.getByRole("button", { name: "Retry streak" }).click();
    await expect(streak.getByText("Longest streak: 23 days")).toBeVisible();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);
  });
}
