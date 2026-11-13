import { test, expect } from "./accounts-fixture";

type Row = {
  rank: number;
  display_name: string;
  weekly_xp: number;
  tie: boolean;
  self: boolean;
  zone: string;
};

const names = [
  "Mina",
  "Kai",
  "Linh",
  "Sam",
  "You",
  "Ana",
  "Ravi",
  "Noor",
  "Tomas",
  "A very long display name that keeps going and going past any reasonable column width",
];
function rows(offset: number, count: number): Row[] {
  return Array.from({ length: count }, (_, i) => {
    const rank = offset + i + 1;
    const xp = rank === 5 || rank === 6 ? 240 : 500 - rank * 10;
    return {
      rank,
      display_name: names[(rank - 1) % names.length],
      weekly_xp: xp,
      tie: rank === 5 || rank === 6,
      self: rank === 5,
      zone: rank <= 5 ? "promotion" : rank >= 26 ? "demotion" : "none",
    };
  });
}
function active(offset: number, limit = 10) {
  const total = 30;
  const count = Math.min(limit, total - offset);
  return {
    state: "active",
    week: { start: "2026-11-09", end: "2026-11-15", days_left: 2 },
    as_of: "2026-11-14T08:42:00Z",
    local_date: "2026-11-14",
    time_zone: "UTC",
    league: {
      name: "Sun",
      tier: 2,
      members: total,
      promotion_top: 5,
      demotion_from: 26,
    },
    self: { rank: 5, weekly_xp: 240, zone: "promotion" },
    rows: rows(offset, count),
    page: {
      offset,
      limit,
      total,
      next_offset: offset + count < total ? offset + count : null,
      prev_offset: offset > 0 ? Math.max(0, offset - limit) : null,
    },
  };
}
const notJoined = {
  state: "not_joined",
  week: { start: "2026-11-09", end: "2026-11-15", days_left: 2 },
  as_of: "2026-11-14T08:42:00Z",
  local_date: "2026-11-14",
  time_zone: "UTC",
  rows: [],
  page: {
    offset: 0,
    limit: 10,
    total: 0,
    next_offset: null,
    prev_offset: null,
  },
};

for (const mobile of [false, true]) {
  test(`join a league, earn XP and flush its cache ${mobile ? "mobile" : "desktop"}`, async ({
    page,
  }) => {
    await page.setViewportSize(
      mobile ? { width: 390, height: 844 } : { width: 1280, height: 900 },
    );
    await page.emulateMedia({
      colorScheme: mobile ? "dark" : "light",
      reducedMotion: "reduce",
    });
    let mode: "none" | "active" | "fail" | "offline" = "none";
    const requested: string[] = [];
    const writes: string[] = [];
    await page.route("**/v1/features", (r) =>
      r.fulfill({ json: { lesson_test_tracer_enabled: true } }),
    );
    await page.route("**/v1/gamification/league**", (r) => {
      const url = new URL(r.request().url());
      if (r.request().method() !== "GET") writes.push(r.request().method());
      requested.push(url.searchParams.get("offset") ?? "0");
      if (mode === "offline") return r.abort("failed");
      if (mode === "fail")
        return r.fulfill({
          status: 503,
          json: { error: "League unavailable. Retry." },
        });
      if (mode === "none") return r.fulfill({ json: notJoined });
      return r.fulfill({
        json: active(Number(url.searchParams.get("offset") ?? 0)),
      });
    });

    // Not joined: no fabricated rank, optional join through a lesson.
    await page.goto("/leagues");
    const league = page.getByRole("region", { name: "Weekly league" });
    await expect(league.getByText("Complete a lesson to join")).toBeVisible();
    await expect(
      league.getByText("It's optional. Your streak doesn't depend on it."),
    ).toBeVisible();
    await expect(
      league.getByRole("link", { name: "Start a lesson" }),
    ).toBeVisible();
    await expect(league.getByRole("table")).toHaveCount(0);

    // XP earned elsewhere joins the league; the same view now ranks.
    mode = "active";
    await page.reload();
    await expect(
      league.getByRole("heading", { name: "Sun league" }),
    ).toBeVisible();
    await expect(
      league.getByText("Ends Sun, Nov 15 · 2 days left"),
    ).toBeVisible();
    await expect(
      league.getByText(
        "Every completed lesson adds to your week. Top 5 move up.",
      ),
    ).toBeVisible();
    await expect(
      league.getByText("Your rank: 5 · 240 XP · promotion zone"),
    ).toBeVisible();
    const self = league.getByRole("row", { name: /^5 You/ });
    await expect(self).toHaveAttribute("aria-current", "true");
    await expect(self).toContainText("240 XP · tie");
    await expect(league.getByRole("row", { name: /^6 Ana/ })).toContainText(
      "240 XP · tie",
    );
    await expect(
      league.getByText("Promotion zone above (ranks 1–5)"),
    ).toBeVisible();
    await expect(league.getByText("Showing ranks 1–10 of 30")).toBeVisible();
    await expect(league.getByText(/credit/i)).toHaveCount(0);

    // Keyboard paging reaches the demotion boundary; touch uses the same button.
    const next = league.getByRole("button", { name: "Next ranks" });
    await next.focus();
    await page.keyboard.press("Enter");
    await expect(league.getByText("Showing ranks 11–20 of 30")).toBeVisible();
    await league.getByRole("button", { name: "Next ranks" }).click();
    await expect(league.getByText("Showing ranks 21–30 of 30")).toBeVisible();
    await expect(
      league.getByText("Demotion zone below (ranks 26–30)"),
    ).toBeVisible();
    await expect(
      league.getByRole("button", { name: "Next ranks" }),
    ).toBeDisabled();
    await league.getByRole("button", { name: "Previous ranks" }).click();
    expect(requested).toContain("10");
    expect(requested).toContain("20");

    // Cache flush (Redis) is invisible: the identical ranking is served after reload.
    await page.reload();
    await expect(league.getByRole("row", { name: /^5 You/ })).toContainText(
      "240 XP",
    );

    // Last week: no final rank or rollover outcome before E5-05.
    await league.getByRole("button", { name: "Last week" }).click();
    await expect(league.getByText("Week is closing")).toBeVisible();
    await expect(
      league.getByText(
        "Final ranks appear after every time zone finishes the day.",
      ),
    ).toBeVisible();
    await expect(league.getByText(/promoted|demoted|stayed/i)).toHaveCount(0);
    await league.getByRole("button", { name: "This week" }).click();

    // Offline: cached ranks with their as-of time, then recovery.
    mode = "offline";
    await page.reload();
    await expect(
      league.getByText(
        "Ranks as of 08:42. They'll refresh when you reconnect.",
      ),
    ).toBeVisible();
    await expect(league.getByRole("row", { name: /^5 You/ })).toBeVisible();
    mode = "active";
    await league.getByRole("button", { name: "Retry leagues" }).click();
    await expect(league.getByText(/Ranks as of/)).toHaveCount(0);

    // Error without a cache offers Retry.
    await page.evaluate(() => localStorage.clear());
    mode = "fail";
    await page.reload();
    await expect(page.getByRole("alert")).toContainText("League unavailable");
    mode = "active";
    await page.getByRole("button", { name: "Retry leagues" }).click();
    await expect(
      league.getByRole("heading", { name: "Sun league" }),
    ).toBeVisible();
    expect(writes).toEqual([]);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);
  });
}
