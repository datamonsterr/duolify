import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { authRequest } from "../accounts/session";
import { RailCreditsCard, RailStreakCard, useWallet } from "./RailCards";

vi.mock("../accounts/session", () => {
  const authRequest = vi.fn();
  return {
    authRequest,
    session: {
      request: (path: string, method = "GET", body?: unknown) =>
        authRequest(path, body, "test-access-token", method),
    },
  };
});
vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, to }: { children: ReactNode; to: string }) => (
    <a href={to}>{children}</a>
  ),
}));

const wallet = {
  available: 1240,
  purchased: 500,
  subscription: 0,
  reserved: 0,
  daily_free: {
    available: 740,
    granted: 1000,
    resets_at: "2026-11-24T00:00:00Z",
    timezone: "UTC",
  },
  recent_activity: [],
};

function Credits({ revision = "/" }: { revision?: string }) {
  const { data } = useWallet(revision);
  return <RailCreditsCard wallet={data} />;
}

describe("shell right rail", () => {
  beforeEach(() => vi.mocked(authRequest).mockReset());

  it("shows the learner's real balance from the wallet endpoint", async () => {
    vi.mocked(authRequest).mockResolvedValue(wallet);
    render(<Credits />);
    expect(await screen.findByText("1,240 credits")).toBeVisible();
    expect(screen.getByText("Daily free 740 · Purchased 500")).toBeVisible();
    expect(screen.queryByText("Balance unavailable")).toBeNull();
    expect(vi.mocked(authRequest).mock.calls[0][0]).toBe("/v1/me/credits");
    expect(vi.mocked(authRequest).mock.calls[0][2]).toBe("test-access-token");
  });

  it("says the balance is unavailable when the wallet cannot be read", async () => {
    vi.mocked(authRequest).mockRejectedValueOnce(new Error("offline"));
    render(<Credits />);
    await waitFor(() => expect(authRequest).toHaveBeenCalled());
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(screen.getByText("Balance unavailable")).toBeVisible();
  });

  it("lists check-in credits when the wallet has them", async () => {
    vi.mocked(authRequest).mockResolvedValue({ ...wallet, check_in: 400 });
    render(<Credits />);
    expect(
      await screen.findByText("Daily free 740 · Check-in 400 · Purchased 500"),
    ).toBeVisible();
  });

  it("treats a malformed wallet response as unavailable", async () => {
    vi.mocked(authRequest).mockResolvedValue({ available: 5 });
    render(<Credits />);
    await waitFor(() => expect(authRequest).toHaveBeenCalled());
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(screen.getByText("Balance unavailable")).toBeVisible();
  });

  it("re-reads the wallet after navigation", async () => {
    vi.mocked(authRequest).mockResolvedValue(wallet);
    const { rerender } = render(<Credits revision="/courses" />);
    await screen.findByText("1,240 credits");
    vi.mocked(authRequest).mockResolvedValue({ ...wallet, available: 1100 });
    rerender(<Credits revision="/credits" />);
    expect(await screen.findByText("1,100 credits")).toBeVisible();
  });

  it("shows the learner's streak instead of an example", async () => {
    vi.mocked(authRequest).mockResolvedValue({
      current_days: 4,
      today_done: true,
    });
    render(<RailStreakCard revision="/" />);
    expect(await screen.findByText("4 day streak")).toBeVisible();
    expect(screen.getByText("Today is done")).toBeVisible();
    expect(screen.queryByText(/Example/)).toBeNull();
    expect(vi.mocked(authRequest).mock.calls[0][0]).toBe(
      "/v1/gamification/streak",
    );
  });
});
