import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { DailyGoal } from "./DailyGoal";
import { authRequest } from "../accounts/session";

vi.mock("../accounts/session", () => ({ authRequest: vi.fn() }));
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
  beforeEach(() => vi.mocked(authRequest).mockReset());
  it("uses configured options and persists a goal without claiming unearned progress", async () => {
    vi.mocked(authRequest)
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
    expect(authRequest).toHaveBeenLastCalledWith(
      "/v1/gamification",
      { daily_goal: 46 },
      undefined,
      "PUT",
    );
  });
  it("surfaces errors and retries without showing static XP", async () => {
    vi.mocked(authRequest)
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
