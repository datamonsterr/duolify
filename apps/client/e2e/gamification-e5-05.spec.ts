import { test, expect } from "./accounts-fixture";

type Last = Record<string, unknown>;
const week = (start: string, end: string) => ({ start, end });
const final = (over: Last): Last => ({
  state: "final",
  week: week("2026-11-02", "2026-11-08"),
  league: "Dawn",
  tier: 1,
  rank: 3,
  members: 30,
  weekly_xp: 340,
  outcome: "promoted",
  next_league: "Sun",
  ...over,
});
const base = (last: Last | undefined) => ({
  state: "active",
  week: { start: "2026-11-09", end: "2026-11-15", days_left: 2 },
  as_of: "2026-11-14T08:42:00Z",
  local_date: "2026-11-14",
  time_zone: "UTC",
  league: {
    name: "Sun",
    tier: 2,
    members: 2,
    promotion_top: 1,
    demotion_from: 2,
  },
  self: { rank: 1, weekly_xp: 240, zone: "promotion" },
  rows: [
    {
      rank: 1,
      display_name: "You",
      weekly_xp: 240,
      tie: false,
      self: true,
      zone: "promotion",
    },
    {
      rank: 2,
      display_name: "Kai",
      weekly_xp: 100,
      tie: false,
      self: false,
      zone: "demotion",
    },
  ],
  page: {
    offset: 0,
    limit: 10,
    total: 2,
    next_offset: null,
    prev_offset: null,
  },
  ...(last ? { last_week: last } : {}),
});

for (const mobile of [false, true]) {
  test(`roll over a week twice and rebuild restored state ${mobile ? "mobile" : "desktop"}`, async ({
    page,
  }) => {
    await page.setViewportSize(
      mobile ? { width: 390, height: 844 } : { width: 1280, height: 900 },
    );
    await page.emulateMedia({
      colorScheme: mobile ? "dark" : "light",
      reducedMotion: "reduce",
    });
    let last: Last | undefined = {
      state: "closing",
      week: week("2026-11-02", "2026-11-08"),
    };
    let offline = false;
    const writes: string[] = [];
    await page.route("**/v1/features", (r) =>
      r.fulfill({ json: { lesson_test_tracer_enabled: true } }),
    );
    await page.route("**/v1/gamification/league**", (r) => {
      if (r.request().method() !== "GET") writes.push(r.request().method());
      if (offline) return r.abort("failed");
      return r.fulfill({ json: base(last) });
    });
    const open = async () => {
      const league = page.getByRole("region", { name: "Weekly league" });
      const tab = league.getByRole("button", { name: "Last week" });
      if (mobile) await tab.click();
      else {
        await tab.focus();
        await page.keyboard.press("Enter");
      }
      return league;
    };

    // Before finalization: only "Week is closing", no outcome words.
    await page.goto("/leagues");
    let league = await open();
    await expect(league.getByText("Week is closing")).toBeVisible();
    await expect(league.getByText(/promoted|demoted|stayed/i)).toHaveCount(0);

    // First rollover: promoted, with the finished rank and XP.
    last = final({});
    await page.reload();
    league = await open();
    await expect(league.getByText("Promoted to Sun league")).toBeVisible();
    await expect(
      league.getByText("Last week: rank 3 of 30 · 340 XP in Dawn league"),
    ).toBeVisible();
    await expect(league.getByText("Nov 2 – Nov 8")).toBeVisible();

    // Second rollover: the newer week replaces it (demoted).
    last = final({
      week: week("2026-11-09", "2026-11-15"),
      league: "Sun",
      tier: 2,
      rank: 29,
      weekly_xp: 10,
      outcome: "demoted",
      next_league: "Dawn",
    });
    await page.reload();
    league = await open();
    await expect(league.getByText("Demoted to Dawn league")).toBeVisible();
    await expect(
      league.getByText("Last week: rank 29 of 30 · 10 XP in Sun league"),
    ).toBeVisible();

    // Stayed outcome and long league names wrap without overflow.
    last = final({
      outcome: "stayed",
      league: "Sun",
      next_league: "Sun",
      rank: 15,
    });
    await page.reload();
    league = await open();
    await expect(league.getByText("Stayed in Sun league")).toBeVisible();

    // Restored identical state renders identically; rewards and Credits never appear.
    const before = await league.innerText();
    await page.reload();
    league = await open();
    expect(await league.innerText()).toBe(before);
    await expect(league.getByText(/credit|reward|bonus/i)).toHaveCount(0);

    // No league last week.
    last = { state: "none", week: week("2026-11-02", "2026-11-08") };
    await page.reload();
    league = await open();
    await expect(
      league.getByText("You weren't in a league last week."),
    ).toBeVisible();

    // Offline keeps the cached prior-week result with the as-of time.
    last = final({});
    await page.reload();
    league = await open();
    await expect(league.getByText("Promoted to Sun league")).toBeVisible();
    offline = true;
    await page.reload();
    league = await open();
    await expect(league.getByText("Promoted to Sun league")).toBeVisible();
    await expect(
      league.getByText(
        "Ranks as of 08:42. They'll refresh when you reconnect.",
      ),
    ).toBeVisible();

    expect(writes).toEqual([]);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);
  });
}
