import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { WalletView } from "./WalletView";
import type { WalletResponse } from "./types";
import { configureAnalytics } from "../../lib/analytics";

const posthog = vi.hoisted(() => ({
  capture: vi.fn(),
  has_opted_out_capturing: vi.fn(() => false),
  opt_in_capturing: vi.fn(),
  opt_out_capturing: vi.fn(),
  identify: vi.fn(),
}));

vi.mock("posthog-js/dist/module.no-external", () => ({ default: posthog }));

describe("read_an_auditable_credit_wallet unit tests", () => {
  const sampleWallet: WalletResponse = {
    available: 212,
    daily_free: {
      available: 28,
      granted: 46,
      resets_at: "2026-11-10T00:00:00+07:00",
      timezone: "Asia/Ho_Chi_Minh",
    },
    purchased: 124,
    subscription: 60,
    reserved: 55,
    reserved_note: "55 reserved for a lesson being prepared",
    recent_activity: [
      {
        id: 4,
        date: "Today 14:02",
        activity: "Reserved · lesson 3",
        source: "Purchased",
        amount_display: "−190 held",
        amount: -190,
        kind: "reserve",
      },
      {
        id: 3,
        date: "Today 13:40",
        activity: "Released · unused outline reserve",
        source: "Purchased",
        amount_display: "+3",
        amount: 3,
        kind: "release",
      },
      {
        id: 2,
        date: "Today 11:20",
        activity: "Settled · tutor chat",
        source: "Daily free",
        amount_display: "−8",
        amount: -8,
        kind: "spend",
      },
      {
        id: 1,
        date: "Nov 7",
        activity: "Purchase · 400 pack · verified",
        source: "Web card",
        amount_display: "+400",
        amount: 400,
        kind: "purchase",
      },
    ],
  };

  beforeEach(() => {
    vi.clearAllMocks();
    configureAnalytics({
      learnerId: "8c940797-a555-421e-8dad-b4df96b02214",
      optedOut: false,
      platform: "web",
    });
  });

  it("renders desktop and mobile matching S16-Wallet with accessible meter and signs", () => {
    const onViewOptions = vi.fn();
    render(<WalletView wallet={sampleWallet} onViewOptions={onViewOptions} />);

    // Available
    expect(screen.getByText("Available")).toBeInTheDocument();
    expect(screen.getByText("212")).toBeInTheDocument();

    // Daily free meter
    expect(screen.getByText("28 / 46")).toBeInTheDocument();
    const meter = screen.getByRole("meter", { name: "Daily free credits" });
    expect(meter).toHaveAttribute("aria-valuenow", "28");
    expect(meter).toHaveAttribute("aria-valuemax", "46");

    // Purchased & Subscription
    expect(screen.getByText("124")).toBeInTheDocument();
    expect(screen.getByText("60")).toBeInTheDocument();

    // Reserved
    expect(screen.getByText("55")).toBeInTheDocument();

    // Textual signs for activity
    expect(screen.getByText("−190 held")).toBeInTheDocument();
    expect(screen.getByText("+3")).toBeInTheDocument();
    expect(screen.getByText("−8")).toBeInTheDocument();
    expect(screen.getByText("+400")).toBeInTheDocument();

    // View credit options click
    const btn = screen.getByRole("button", { name: "View credit options" });
    fireEvent.click(btn);
    expect(onViewOptions).toHaveBeenCalled();

    // Analytics: ui_screen_viewed for credits must be emitted, never reward_credits_granted
    expect(posthog.capture).toHaveBeenCalledWith(
      "ui_screen_viewed",
      expect.objectContaining({ screen_id: "credits" }),
    );
    expect(posthog.capture).not.toHaveBeenCalledWith(
      "reward_credits_granted",
      expect.anything(),
    );
  });

  it("renders daily free exhausted state with recovery guidance", () => {
    const exhaustedWallet: WalletResponse = {
      available: 12,
      daily_free: {
        available: 0,
        granted: 46,
        resets_at: "2026-11-10T00:00:00+07:00",
        timezone: "Asia/Ho_Chi_Minh",
      },
      purchased: 12,
      subscription: 0,
      reserved: 0,
      reserved_note: "Purchased only · 0 reserved",
      recent_activity: [],
    };

    render(<WalletView wallet={exhaustedWallet} />);
    expect(screen.getByText("0 / 46")).toBeInTheDocument();
    expect(
      screen.getByText("Your notes and lessons stay available"),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Wait for daily reset" }),
    ).toBeInTheDocument();
  });

  it("renders offline unverified cache state", () => {
    const offlineWallet: WalletResponse = {
      available: 0,
      daily_free: {
        available: 0,
        granted: 46,
        resets_at: "2026-11-10T00:00:00+07:00",
        timezone: "Asia/Ho_Chi_Minh",
      },
      purchased: 0,
      subscription: 0,
      reserved: 0,
      cached: true,
      unverified: true,
      last_verified_at: "08:42",
      recent_activity: [
        {
          id: 2,
          date: "Nov 8 · end of day",
          activity: "Expired · daily free",
          source: "Daily free",
          amount_display: "−6",
          amount: -6,
          kind: "expire",
        },
        {
          id: 1,
          date: "Nov 8",
          activity: "Settled · lesson 2",
          source: "Purchased",
          amount_display: "−190",
          amount: -190,
          kind: "spend",
        },
      ],
    };

    render(<WalletView wallet={offlineWallet} />);
    expect(screen.getByText("Balance not verified")).toBeInTheDocument();
    expect(screen.getByText("Reset awaiting sync")).toBeInTheDocument();
    expect(screen.getByText("−6")).toBeInTheDocument();
    expect(screen.getByText("−190")).toBeInTheDocument();
  });

  it("renders empty state when no activity exists", () => {
    const emptyWallet: WalletResponse = {
      available: 0,
      daily_free: {
        available: 0,
        granted: 46,
        resets_at: "2026-11-10T00:00:00+07:00",
        timezone: "UTC",
      },
      purchased: 0,
      subscription: 0,
      reserved: 0,
      recent_activity: [],
    };

    render(<WalletView wallet={emptyWallet} />);
    expect(screen.getByText("No wallet activity yet.")).toBeInTheDocument();
  });
});
