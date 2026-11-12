import { render, screen, fireEvent, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { LeaguePanel } from "./League";
import { authRequest } from "../accounts/session";
import { configureUiAnalytics, trackScreen } from "../../lib/analytics";
import { screenForPath } from "../shell/navigation";

vi.mock("../accounts/session", () => ({ authRequest: vi.fn() }));
const posthog = vi.hoisted(() => ({
  capture: vi.fn(),
  has_opted_out_capturing: vi.fn(() => false),
  init: vi.fn(),
}));
vi.mock("posthog-js/dist/module.no-external", () => ({ default: posthog }));

const row = (rank: number, name: string, xp: number, extra = {}) => ({
  rank,
  display_name: name,
  weekly_xp: xp,
  tie: false,
  self: false,
  zone: "none",
  ...extra,
});
const active = {
  state: "active",
  week: { start: "2026-11-09", end: "2026-11-15", days_left: 1 },
  as_of: "2026-11-15T08:42:00Z",
  local_date: "2026-11-15",
  time_zone: "UTC",
  league: {
    name: "Sun",
    tier: 2,
    members: 3,
    promotion_top: 1,
    demotion_from: 3,
  },
  self: { rank: 2, weekly_xp: 240, zone: "none" },
  rows: [
    row(1, "Mina", 420, { zone: "promotion" }),
    row(2, "You", 240, { self: true, tie: true }),
    row(3, "Ana", 240, { tie: true, zone: "demotion" }),
  ],
  page: {
    offset: 0,
    limit: 10,
    total: 3,
    next_offset: null,
    prev_offset: null,
  },
};
const notJoined = {
  state: "not_joined",
  week: active.week,
  as_of: active.as_of,
  local_date: active.local_date,
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

describe("weekly league panel", () => {
  beforeEach(() => {
    vi.mocked(authRequest).mockReset();
    localStorage.clear();
  });
  afterEach(() => {
    configureUiAnalytics(null);
    vi.clearAllMocks();
  });
  it("labels self, ties and text zone boundaries with a singular day count", async () => {
    vi.mocked(authRequest).mockResolvedValue(active);
    render(<LeaguePanel />);
    expect(
      await screen.findByRole("heading", { name: "Sun league" }),
    ).toBeVisible();
    expect(screen.getByText("Ends Sun, Nov 15 · 1 day left")).toBeVisible();
    expect(screen.getByText("Your rank: 2 · 240 XP")).toBeVisible();
    const self = screen.getByRole("row", { name: /^2 You/ });
    expect(self).toHaveAttribute("aria-current", "true");
    expect(within(self).getByText("240 XP · tie")).toBeVisible();
    expect(screen.getByText("Promotion zone above (rank 1)")).toBeVisible();
    expect(screen.getByText("Demotion zone below (rank 3)")).toBeVisible();
    expect(screen.getByText("Showing ranks 1–3 of 3")).toBeVisible();
    expect(screen.getByRole("button", { name: "Next ranks" })).toBeDisabled();
    expect(screen.queryByText(/credit/i)).toBeNull();
  });
  it("requests the next page offset and keeps labelled rows", async () => {
    vi.mocked(authRequest).mockResolvedValueOnce({
      ...active,
      page: { ...active.page, next_offset: 3, total: 6 },
    });
    render(<LeaguePanel />);
    vi.mocked(authRequest).mockResolvedValueOnce({
      ...active,
      rows: [row(4, "Ravi", 100)],
      page: {
        offset: 3,
        limit: 10,
        total: 6,
        next_offset: null,
        prev_offset: 0,
      },
    });
    fireEvent.click(await screen.findByRole("button", { name: "Next ranks" }));
    expect(await screen.findByText("Showing ranks 4–4 of 6")).toBeVisible();
    expect(vi.mocked(authRequest).mock.calls.at(-1)?.[0]).toBe(
      "/v1/gamification/league?offset=3",
    );
  });
  it("invites a Learner who has not joined without inventing a rank", async () => {
    vi.mocked(authRequest).mockResolvedValue(notJoined);
    render(<LeaguePanel />);
    expect(await screen.findByText("Complete a lesson to join")).toBeVisible();
    expect(screen.getByRole("link", { name: "Start a lesson" })).toBeVisible();
    expect(screen.queryByRole("table")).toBeNull();
    expect(screen.queryByText(/Your rank/)).toBeNull();
  });
  it("shows last week as closing with no outcome", async () => {
    vi.mocked(authRequest).mockResolvedValue(active);
    render(<LeaguePanel />);
    fireEvent.click(await screen.findByRole("button", { name: "Last week" }));
    expect(screen.getByText("Week is closing")).toBeVisible();
    expect(screen.queryByText(/promoted|demoted|stayed/i)).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "This week" }));
    expect(screen.getByRole("table")).toBeVisible();
  });
  it("falls back to cached ranks with their as-of time and recovers", async () => {
    vi.mocked(authRequest).mockResolvedValueOnce(active);
    const { unmount } = render(<LeaguePanel />);
    await screen.findByRole("table");
    unmount();
    vi.mocked(authRequest).mockRejectedValueOnce(new Error("offline"));
    render(<LeaguePanel />);
    expect(
      await screen.findByText(
        "Ranks as of 08:42. They'll refresh when you reconnect.",
      ),
    ).toBeVisible();
    expect(screen.getByRole("table")).toBeVisible();
    vi.mocked(authRequest).mockResolvedValueOnce(active);
    fireEvent.click(screen.getByRole("button", { name: "Retry leagues" }));
    await screen.findByRole("heading", { name: "Sun league" });
    expect(screen.queryByText(/Ranks as of/)).toBeNull();
  });
  it("offers Retry when nothing is cached", async () => {
    vi.mocked(authRequest).mockRejectedValueOnce(
      new Error("League unavailable. Retry."),
    );
    render(<LeaguePanel />);
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "League unavailable",
    );
    vi.mocked(authRequest).mockResolvedValueOnce(notJoined);
    fireEvent.click(screen.getByRole("button", { name: "Retry leagues" }));
    expect(await screen.findByText("Complete a lesson to join")).toBeVisible();
  });
  it("wraps long names without a fixed width", async () => {
    const long = "L".repeat(120);
    vi.mocked(authRequest).mockResolvedValue({
      ...active,
      rows: [row(1, long, 5)],
    });
    render(<LeaguePanel />);
    expect(await screen.findByText(long)).toBeVisible();
  });
});

describe("leagues screen analytics", () => {
  const context = {
    enabled: true,
    learnerId: "7d3d37e8-4c06-4a0d-b6dc-0d0f3d2ea111",
    platform: "desktop" as const,
    appVersion: "0.1.0",
  };
  afterEach(() => {
    configureUiAnalytics(null);
    vi.clearAllMocks();
  });
  it("maps /leagues to the catalogued screen and delivers once through the double", () => {
    expect(screenForPath("/leagues")).toBe("leagues");
    configureUiAnalytics(context);
    trackScreen("leagues");
    expect(posthog.capture).toHaveBeenCalledTimes(1);
    const [name, props] = posthog.capture.mock.calls[0];
    expect(name).toBe("ui_screen_viewed");
    expect(props).toMatchObject({
      distinct_id: context.learnerId,
      screen_id: "leagues",
      platform: "desktop",
      source: "client",
    });
    expect(JSON.stringify(props)).not.toMatch(/Mina|Ana|XP|rank/i);
  });
  it("sends nothing when opted out or without a UUID", () => {
    configureUiAnalytics({ ...context, enabled: false });
    trackScreen("leagues");
    configureUiAnalytics({ ...context, learnerId: "person@example.com" });
    trackScreen("leagues");
    expect(posthog.capture).not.toHaveBeenCalled();
  });
});
