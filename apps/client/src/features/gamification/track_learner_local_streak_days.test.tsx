import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { StreakPanel } from "./Streak";
import { authRequest } from "../accounts/session";

vi.mock("../accounts/session", () => ({ authRequest: vi.fn() }));
const base = {
  current_days: 7,
  longest_days: 23,
  today_done: false,
  local_date: "2026-11-09",
  time_zone: "Asia/Ho_Chi_Minh",
  as_of: "2026-11-09T08:42:00Z",
  rollover_pending: false,
  month: "2026-11",
  calendar: [
    { date: "2026-11-05", state: "missed" },
    { date: "2026-11-06", state: "done" },
    { date: "2026-11-09", state: "today" },
  ],
};
describe("learner-local streak panel", () => {
  beforeEach(() => vi.mocked(authRequest).mockReset());
  it("shows server streak, labelled calendar marks and a keyboard list alternative", async () => {
    vi.mocked(authRequest).mockResolvedValue(base);
    render(<StreakPanel />);
    expect(await screen.findByText("Longest streak: 23 days")).toBeVisible();
    expect(screen.getByText("Today isn't done yet")).toBeVisible();
    expect(
      screen.getByText("Finish one lesson before the day ends to make it 8."),
    ).toBeVisible();
    expect(screen.getByRole("cell", { name: "5 Missed" })).toBeVisible();
    expect(screen.getByRole("cell", { name: "9 Today" })).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Show as list" }));
    expect(screen.getByText("Nov 5, 2026: Missed")).toBeVisible();
  });
  it("shows reset, empty and pending rollover states from the server answer", async () => {
    vi.mocked(authRequest).mockResolvedValueOnce({
      ...base,
      current_days: 0,
    });
    const { unmount } = render(<StreakPanel />);
    expect(await screen.findByText("Welcome back")).toBeVisible();
    expect(screen.getByText(/longest streak \(23 days\)/)).toBeVisible();
    unmount();
    vi.mocked(authRequest).mockResolvedValueOnce({
      ...base,
      current_days: 0,
      longest_days: 0,
    });
    const second = render(<StreakPanel />);
    expect(await screen.findByText("Start your streak")).toBeVisible();
    second.unmount();
    vi.mocked(authRequest).mockResolvedValueOnce({
      ...base,
      rollover_pending: true,
    });
    render(<StreakPanel />);
    expect(await screen.findByText("Rollover in progress")).toBeVisible();
    expect(screen.getByText(/Streak as of/)).toBeVisible();
  });
  it("requests another month and recovers from errors", async () => {
    vi.mocked(authRequest)
      .mockRejectedValueOnce(new Error("Streak unavailable. Retry."))
      .mockResolvedValueOnce(base)
      .mockResolvedValueOnce({ ...base, month: "2026-10", calendar: [] });
    render(<StreakPanel />);
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Streak unavailable",
    );
    fireEvent.click(screen.getByRole("button", { name: "Retry streak" }));
    await screen.findByText("Longest streak: 23 days");
    fireEvent.click(screen.getByRole("button", { name: "Previous month" }));
    await waitFor(() =>
      expect(authRequest).toHaveBeenLastCalledWith(
        "/v1/gamification/streak?month=2026-10",
        undefined,
        undefined,
        "GET",
      ),
    );
  });
});
