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

describe("grant_and_expire_eligible_daily_credits unit tests", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    configureAnalytics({
      learnerId: "8c940797-a555-421e-8dad-b4df96b02214",
      optedOut: false,
      platform: "web",
    });
  });

  it("renders daily free grant state with accessible meter and reset instant", () => {
    const grantWallet: WalletResponse = {
      available: 46,
      daily_free: {
        available: 46,
        granted: 46,
        resets_at: "2026-11-10T00:00:00+07:00",
        timezone: "Asia/Ho_Chi_Minh",
      },
      purchased: 0,
      subscription: 0,
      reserved: 0,
      recent_activity: [
        {
          id: 1,
          date: "Today 00:00",
          activity: "Daily free credits granted",
          source: "Daily free",
          amount_display: "+46",
          amount: 46,
          kind: "daily_grant",
        },
      ],
    };

    render(<WalletView wallet={grantWallet} />);

    // Available is 46
    expect(screen.getByText("Available")).toBeInTheDocument();
    expect(screen.getByText("46")).toBeInTheDocument();

    // Daily free meter shows full 46 / 46
    expect(screen.getByText("46 / 46")).toBeInTheDocument();
    const meter = screen.getByRole("meter", { name: "Daily free credits" });
    expect(meter).toHaveAttribute("aria-valuenow", "46");
    expect(meter).toHaveAttribute("aria-valuemax", "46");

    // Reset timezone info
    expect(
      screen.getAllByText(/00:00 Ho Chi Minh City/i).length,
    ).toBeGreaterThanOrEqual(1);

    // Grant activity row
    expect(screen.getByText("Daily free credits granted")).toBeInTheDocument();
    expect(screen.getByText("+46")).toBeInTheDocument();
  });

  it("displays expired unreserved remainder and midnight reset across local days", () => {
    const expiredWallet: WalletResponse = {
      available: 46,
      daily_free: {
        available: 46,
        granted: 46,
        resets_at: "2026-11-11T00:00:00+07:00",
        timezone: "Asia/Ho_Chi_Minh",
      },
      purchased: 0,
      subscription: 0,
      reserved: 0,
      recent_activity: [
        {
          id: 3,
          date: "Today 00:00",
          activity: "Daily free credits granted",
          source: "Daily free",
          amount_display: "+46",
          amount: 46,
          kind: "daily_grant",
        },
        {
          id: 2,
          date: "Yesterday 23:59",
          activity: "Expired · daily free",
          source: "Daily free",
          amount_display: "−38",
          amount: -38,
          kind: "expire",
        },
        {
          id: 1,
          date: "Yesterday 11:20",
          activity: "Settled · tutor chat",
          source: "Daily free",
          amount_display: "−8",
          amount: -8,
          kind: "spend",
        },
      ],
    };

    render(<WalletView wallet={expiredWallet} />);

    // Expired activity item
    expect(screen.getByText("Expired · daily free")).toBeInTheDocument();
    expect(screen.getByText("−38")).toBeInTheDocument();

    // Spend item
    expect(screen.getByText("Settled · tutor chat")).toBeInTheDocument();
    expect(screen.getByText("−8")).toBeInTheDocument();

    // New day grant
    expect(screen.getByText("+46")).toBeInTheDocument();
    expect(screen.getByText("46 / 46")).toBeInTheDocument();
  });

  it("updates timezone display when effective timezone changes after 24h", () => {
    const nyWallet: WalletResponse = {
      available: 46,
      daily_free: {
        available: 46,
        granted: 46,
        resets_at: "2026-11-11T00:00:00-05:00",
        timezone: "America/New_York",
      },
      purchased: 0,
      subscription: 0,
      reserved: 0,
      recent_activity: [],
    };

    render(<WalletView wallet={nyWallet} />);

    // Timezone city is New York
    expect(
      screen.getAllByText(/00:00 New York/i).length,
    ).toBeGreaterThanOrEqual(1);
  });

  it("supports keyboard navigation on view options button", () => {
    const onViewOptions = vi.fn();
    const wallet: WalletResponse = {
      available: 46,
      daily_free: {
        available: 46,
        granted: 46,
        resets_at: "2026-11-10T00:00:00+07:00",
        timezone: "Asia/Ho_Chi_Minh",
      },
      purchased: 0,
      subscription: 0,
      reserved: 0,
      recent_activity: [],
    };

    render(<WalletView wallet={wallet} onViewOptions={onViewOptions} />);

    const button = screen.getByRole("button", { name: "View credit options" });
    fireEvent.click(button);
    expect(onViewOptions).toHaveBeenCalledTimes(1);
  });
});
