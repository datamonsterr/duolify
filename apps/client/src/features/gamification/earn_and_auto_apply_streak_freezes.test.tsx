import { render, screen, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { StreakPanel } from "./Streak";
import { authRequest } from "../accounts/session";

vi.mock("../accounts/session", () => ({ authRequest: vi.fn() }));
const base = {
  current_days: 7,
  longest_days: 7,
  today_done: false,
  local_date: "2026-11-09",
  time_zone: "UTC",
  as_of: "2026-11-09T08:42:00Z",
  rollover_pending: false,
  month: "2026-11",
  calendar: [
    { date: "2026-11-07", state: "done" },
    { date: "2026-11-08", state: "frozen" },
    { date: "2026-11-09", state: "today" },
  ],
  freezes: { held: 0, cap: 2, milestone_days: 7, next_milestone_in: 7 },
  last_earned: { reached_on: "2026-11-07", streak_length: 7 },
  notices: [{ protected_on: "2026-11-08", remaining: 0, streak_length: 7 }],
};
describe("streak freeze inventory and auto-apply notice", () => {
  beforeEach(() => vi.mocked(authRequest).mockReset());
  it("shows inventory, effort rule, dated notice and a labelled frozen day", async () => {
    vi.mocked(authRequest).mockResolvedValue(base);
    render(<StreakPanel />);
    expect(await screen.findByText("Streak freezes: 0 of 2")).toBeVisible();
    expect(screen.getByText("Earned every 7 days of streak")).toBeVisible();
    expect(screen.getByText("Next freeze in 7 days")).toBeVisible();
    expect(screen.getByText("A freeze covered Nov 8, 2026")).toBeVisible();
    expect(
      screen.getByText("Your streak stayed at 7. No freezes left."),
    ).toBeVisible();
    expect(
      screen.getByText("Freeze earned on Nov 7, 2026 at a 7-day streak"),
    ).toBeVisible();
    expect(
      screen.getByRole("cell", { name: "8 Freeze covered" }),
    ).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Show as list" }));
    expect(screen.getByText("Nov 8, 2026: Freeze covered")).toBeVisible();
  });
  it("explains a full inventory without any credit substitute", async () => {
    vi.mocked(authRequest).mockResolvedValue({
      ...base,
      notices: [],
      last_earned: null,
      freezes: { held: 2, cap: 2, milestone_days: 7, next_milestone_in: 3 },
    });
    render(<StreakPanel />);
    expect(await screen.findByText("Streak freezes: 2 of 2")).toBeVisible();
    expect(
      screen.getByText("Freezes are full. Extra milestones earn nothing."),
    ).toBeVisible();
    expect(screen.queryByText(/Next freeze in/)).toBeNull();
    expect(screen.queryByText(/A freeze covered/)).toBeNull();
  });
  it("never offers to buy or activate a freeze", async () => {
    vi.mocked(authRequest).mockResolvedValue({
      ...base,
      freezes: { held: 1, cap: 2, milestone_days: 7, next_milestone_in: 1 },
    });
    render(<StreakPanel />);
    await screen.findByText("Streak freezes: 1 of 2");
    expect(screen.getByText("Next freeze in 1 day")).toBeVisible();
    expect(
      screen.getByText("1 freeze applies automatically on a missed day."),
    ).toBeVisible();
    for (const control of [
      ...screen.queryAllByRole("button"),
      ...screen.queryAllByRole("link"),
    ]) {
      expect(control.textContent).not.toMatch(
        /buy|purchase|activate|use a freeze|credits/i,
      );
    }
  });
  it("tolerates a server answer without freeze fields", async () => {
    const legacy: Record<string, unknown> = { ...base };
    for (const key of ["freezes", "notices", "last_earned"]) delete legacy[key];
    vi.mocked(authRequest).mockResolvedValue(legacy);
    render(<StreakPanel />);
    expect(await screen.findByText("Longest streak: 7 days")).toBeVisible();
    expect(screen.queryByText(/Streak freezes:/)).toBeNull();
  });
});
