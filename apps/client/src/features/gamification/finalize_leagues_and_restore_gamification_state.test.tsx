import { render, screen, fireEvent, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { LeaguePanel } from "./League";
import { authRequest } from "../accounts/session";

vi.mock("../accounts/session", () => ({ authRequest: vi.fn() }));

const base = {
  state: "active",
  week: { start: "2026-11-09", end: "2026-11-15", days_left: 2 },
  as_of: "2026-11-14T08:42:00Z",
  local_date: "2026-11-14",
  time_zone: "UTC",
  league: {
    name: "Sun",
    tier: 2,
    members: 1,
    promotion_top: 0,
    demotion_from: 0,
  },
  self: { rank: 1, weekly_xp: 10, zone: "none" },
  rows: [
    {
      rank: 1,
      display_name: "You",
      weekly_xp: 10,
      tie: false,
      self: true,
      zone: "none",
    },
  ],
  page: {
    offset: 0,
    limit: 10,
    total: 1,
    next_offset: null,
    prev_offset: null,
  },
};
const lastWeek = (over: object) => ({
  state: "final",
  week: { start: "2026-11-02", end: "2026-11-08" },
  league: "Dawn",
  tier: 1,
  rank: 2,
  members: 9,
  weekly_xp: 120,
  outcome: "promoted",
  next_league: "Sun",
  ...over,
});
const show = async (last?: object) => {
  vi.mocked(authRequest).mockResolvedValue({
    ...base,
    ...(last ? { last_week: last } : {}),
  });
  render(<LeaguePanel />);
  await screen.findByRole("heading", { name: "Sun league" });
  fireEvent.click(screen.getByRole("button", { name: "Last week" }));
  return screen.getByRole("region", { name: "Weekly league" });
};

describe("last week result", () => {
  beforeEach(() => localStorage.clear());
  afterEach(() => vi.clearAllMocks());

  it.each([
    [{ outcome: "promoted" }, "Promoted to Sun league"],
    [
      { outcome: "demoted", league: "Sun", next_league: "Dawn" },
      "Demoted to Dawn league",
    ],
    [{ outcome: "stayed", next_league: "Dawn" }, "Stayed in Dawn league"],
  ])("states %j", async (over, text) => {
    const panel = await show(lastWeek(over));
    expect(within(panel).getByText(text)).toBeVisible();
    expect(within(panel).getByText(/rank 2 of 9 · 120 XP/)).toBeVisible();
    expect(within(panel).queryByText(/credit|reward/i)).toBeNull();
  });

  it("keeps the closing note while the week is not final or the API predates results", async () => {
    for (const last of [
      { state: "closing", week: { start: "2026-11-02", end: "2026-11-08" } },
      undefined,
    ]) {
      const panel = await show(last);
      expect(within(panel).getByText("Week is closing")).toBeVisible();
      expect(within(panel).queryByText(/promoted|demoted|stayed/i)).toBeNull();
      screen.getByRole("button", { name: "This week" });
      document.body.innerHTML = "";
    }
  });

  it("explains a week without a league", async () => {
    const panel = await show({
      state: "none",
      week: { start: "2026-11-02", end: "2026-11-08" },
    });
    expect(
      within(panel).getByText("You weren't in a league last week."),
    ).toBeVisible();
  });
});
