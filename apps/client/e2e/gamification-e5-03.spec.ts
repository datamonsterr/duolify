import { test, expect } from "./accounts-fixture";

type Day = { date: string; state: string };

function november(overrides: Record<string, string>): Day[] {
  return Array.from({ length: 30 }, (_, i) => {
    const date = `2026-11-${String(i + 1).padStart(2, "0")}`;
    return { date, state: overrides[date] ?? (i + 1 > 9 ? "future" : "none") };
  });
}

const earned = {
  current_days: 7,
  longest_days: 7,
  today_done: true,
  local_date: "2026-11-07",
  time_zone: "UTC",
  as_of: "2026-11-07T12:42:00Z",
  rollover_pending: false,
  month: "2026-11",
  calendar: november({
    "2026-11-01": "done",
    "2026-11-02": "done",
    "2026-11-03": "done",
    "2026-11-04": "done",
    "2026-11-05": "done",
    "2026-11-06": "done",
    "2026-11-07": "today",
  }),
  freezes: { held: 1, cap: 2, milestone_days: 7, next_milestone_in: 7 },
  last_earned: { reached_on: "2026-11-07", streak_length: 7 },
  notices: [],
};
const covered = {
  ...earned,
  today_done: false,
  local_date: "2026-11-09",
  calendar: november({
    ...Object.fromEntries(
      [1, 2, 3, 4, 5, 6, 7].map((d) => [`2026-11-0${d}`, "done"]),
    ),
    "2026-11-08": "frozen",
    "2026-11-09": "today",
  }),
  freezes: { held: 0, cap: 2, milestone_days: 7, next_milestone_in: 7 },
  notices: [{ protected_on: "2026-11-08", remaining: 0, streak_length: 7 }],
};

for (const mobile of [false, true]) {
  test(`streak freezes earned and auto-applied ${mobile ? "mobile" : "desktop"}`, async ({
    page,
  }) => {
    await page.setViewportSize(
      mobile ? { width: 390, height: 844 } : { width: 1280, height: 900 },
    );
    await page.emulateMedia({
      colorScheme: mobile ? "dark" : "light",
      reducedMotion: "reduce",
    });
    let body: Record<string, unknown> = earned;
    let failed = false;
    const writes: string[] = [];
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
          time_zone: "UTC",
        },
      }),
    );
    await page.route("**/v1/gamification/streak**", (r) => {
      if (r.request().method() !== "GET") writes.push(r.request().method());
      if (failed)
        return r.fulfill({
          status: 503,
          json: { error: "Streak unavailable. Retry." },
        });
      return r.fulfill({ json: body });
    });

    // Milestone earned: inventory, effort rule and next milestone, with no purchase path.
    await page.goto("/streak");
    const streak = page.getByRole("region", { name: "Your streak" });
    await expect(streak.getByText("Streak freezes: 1 of 2")).toBeVisible();
    await expect(
      streak.getByText("Earned every 7 days of streak"),
    ).toBeVisible();
    await expect(
      streak.getByText("Freeze earned on Nov 7, 2026 at a 7-day streak"),
    ).toBeVisible();
    await expect(
      streak.getByText("1 freeze applies automatically on a missed day."),
    ).toBeVisible();
    await expect(
      streak.getByRole("button", { name: /buy|purchase|activate|use/i }),
    ).toHaveCount(0);

    // Missed day with a freeze: persistent dated notice; streak preserved at 7.
    body = covered;
    await page.reload();
    await expect(
      streak.getByText("A freeze covered Nov 8, 2026"),
    ).toBeVisible();
    await expect(
      streak.getByText("Your streak stayed at 7. No freezes left."),
    ).toBeVisible();
    await expect(
      streak.getByRole("cell", { name: "8 Freeze covered" }),
    ).toBeVisible();
    // The notice persists across reloads (server state, not a one-shot toast).
    await page.reload();
    await expect(
      streak.getByText("A freeze covered Nov 8, 2026"),
    ).toBeVisible();

    // Keyboard parity: list alternative lists the covered day.
    const toggle = streak.getByRole("button", { name: "Show as list" });
    await toggle.focus();
    await page.keyboard.press("Enter");
    await expect(
      streak
        .getByRole("listitem")
        .filter({ hasText: "Nov 8, 2026: Freeze covered" }),
    ).toBeVisible();
    await streak.getByRole("button", { name: "Show as calendar" }).click();

    // Cap reached: no credit substitute is offered.
    body = {
      ...earned,
      freezes: { held: 2, cap: 2, milestone_days: 7, next_milestone_in: 3 },
    };
    await page.reload();
    await expect(
      streak.getByText("Freezes are full. Extra milestones earn nothing."),
    ).toBeVisible();
    await expect(page.getByText(/credit/i)).toHaveCount(0);

    // Error with recovery.
    failed = true;
    await page.reload();
    await expect(page.getByRole("alert")).toContainText("Streak unavailable");
    failed = false;
    await page.getByRole("button", { name: "Retry streak" }).click();
    await expect(streak.getByText("Streak freezes: 2 of 2")).toBeVisible();
    expect(writes).toEqual([]);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);
  });
}
