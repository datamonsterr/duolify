import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { authRequest } from "../accounts/session";
import { SidebarStats, useWallet } from "./SidebarStats";

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
  Link: ({
    children,
    to,
    onClick,
    ...rest
  }: {
    children: ReactNode;
    to: string;
    onClick?: () => void;
  }) => (
    <a href={to} onClick={onClick} {...rest}>
      {children}
    </a>
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

function Stats({ revision = "/" }: { revision?: string }) {
  const { data } = useWallet(revision);
  return <SidebarStats wallet={data} revision={revision} />;
}
const progress = { daily_xp: 23, daily_goal: 46 };
function answer(
  pairs: Record<string, unknown>,
): (path: string) => Promise<unknown> {
  return (path) =>
    pairs[path] === undefined
      ? Promise.reject(new Error("offline"))
      : Promise.resolve(pairs[path]);
}

describe("sidebar stats", () => {
  beforeEach(() => {
    vi.mocked(authRequest).mockReset();
  });

  it("shows streak, XP and credits as icon and number links", async () => {
    vi.mocked(authRequest).mockImplementation(
      answer({
        "/v1/me/credits": wallet,
        "/v1/gamification/streak": { current_days: 4, today_done: true },
        "/v1/gamification": progress,
      }),
    );
    render(<Stats />);
    await new Promise((r) => setTimeout(r, 50));
    const streak = await screen.findByRole("link", { name: "4 day streak" });
    expect(streak).toHaveAttribute("href", "/streak");
    expect(streak).toHaveTextContent("4");
    expect(
      await screen.findByRole("link", { name: "23 of 46 XP today" }),
    ).toHaveTextContent("23 / 46");
    const credits = await screen.findByRole("link", {
      name: "1,240 credits",
    });
    expect(credits).toHaveAttribute("href", "/credits");
    expect(credits).toHaveTextContent("1,240");
    expect(screen.queryByText(/unavailable/i)).toBeNull();
    expect(screen.queryByText(/Example/)).toBeNull();
    const paths = vi.mocked(authRequest).mock.calls.map((call) => call[0]);
    expect(paths).toEqual(
      expect.arrayContaining(["/v1/me/credits", "/v1/gamification/streak"]),
    );
    expect(vi.mocked(authRequest).mock.calls[0][2]).toBe("test-access-token");
  });

  it("shows a dash, not a sentence, when a read fails", async () => {
    vi.mocked(authRequest).mockRejectedValue(new Error("offline"));
    render(<Stats />);
    await waitFor(() => expect(authRequest).toHaveBeenCalled());
    await new Promise((resolve) => setTimeout(resolve, 0));
    const links = screen.getAllByRole("link");
    expect(links).toHaveLength(3);
    for (const link of links) expect(link).toHaveTextContent("\u2013");
    expect(screen.getByRole("link", { name: /Credits/ })).toBeVisible();
    expect(screen.queryByText("Balance unavailable")).toBeNull();
  });

  it("treats a malformed wallet response as unavailable", async () => {
    vi.mocked(authRequest).mockResolvedValue({ available: 5 });
    render(<Stats />);
    await waitFor(() => expect(authRequest).toHaveBeenCalled());
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(screen.queryByRole("link", { name: "5 credits" })).toBeNull();
  });

  it("re-reads the wallet after navigation", async () => {
    vi.mocked(authRequest).mockResolvedValue(wallet);
    const { rerender } = render(<Stats revision="/courses" />);
    await screen.findByRole("link", { name: "1,240 credits" });
    vi.mocked(authRequest).mockResolvedValue({ ...wallet, available: 1100 });
    rerender(<Stats revision="/credits" />);
    expect(
      await screen.findByRole("link", { name: "1,100 credits" }),
    ).toBeVisible();
  });
});
