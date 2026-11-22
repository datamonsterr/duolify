import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { DailyGoal } from "./DailyGoal";
import { session } from "../accounts/session";

// Progress is learner data: every call must carry the session token (session.request).
vi.mock("../accounts/session", () => ({ session: { request: vi.fn() } }));
const request = vi.mocked(session.request);
const progress = {
  total_xp: 23,
  daily_xp: 23,
  daily_goal: 23,
  goal_options: [23, 46],
  goal_met: true,
  local_date: "2026-11-10",
  time_zone: "America/New_York",
};
describe("authoritative daily goal", () => {
  beforeEach(() => request.mockReset());
  it("keeps sidebar failures visible without announcing them as form alerts", async () => {
    request.mockRejectedValueOnce(new Error("Progress unavailable. Retry."));
    render(<DailyGoal compact />);
    expect(
      await screen.findByText("Progress unavailable. Retry."),
    ).toBeVisible();
    expect(screen.queryByRole("alert")).toBeNull();
    expect(screen.queryByRole("status")).toBeNull();
    expect(
      screen.getByRole("button", { name: "Reload daily goal" }),
    ).toBeVisible();
  });
  it("uses configured options and persists a goal without claiming unearned progress", async () => {
    request
      .mockResolvedValueOnce(progress)
      .mockResolvedValueOnce({ ...progress, daily_goal: 46, goal_met: false });
    render(<DailyGoal editable />);
    expect(await screen.findByText("Daily goal met")).toBeVisible();
    expect(screen.getAllByRole("option").map((x) => x.textContent)).toEqual([
      "23 XP",
      "46 XP",
    ]);
    fireEvent.change(screen.getByRole("combobox"), { target: { value: "46" } });
    fireEvent.click(screen.getByRole("button", { name: "Save goal" }));
    expect(await screen.findByText("23 / 46 XP today")).toBeVisible();
    expect(request).toHaveBeenNthCalledWith(1, "/v1/gamification");
    expect(request).toHaveBeenLastCalledWith("/v1/gamification", "PUT", {
      daily_goal: 46,
    });
  });
  it("surfaces errors and retries without showing static XP", async () => {
    request
      .mockRejectedValueOnce(new Error("Progress unavailable. Retry."))
      .mockResolvedValueOnce({ ...progress, daily_xp: 0, goal_met: false });
    render(<DailyGoal />);
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Progress unavailable",
    );
    fireEvent.click(screen.getByRole("button", { name: "Retry progress" }));
    await waitFor(() =>
      expect(screen.getByText("0 / 23 XP today")).toBeVisible(),
    );
  });
});
