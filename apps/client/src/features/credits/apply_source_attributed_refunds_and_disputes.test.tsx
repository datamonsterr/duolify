import { render, screen } from "@testing-library/react";
import { expect, it } from "vitest";
import { WalletView } from "./WalletView";
import type { WalletResponse } from "./types";

it("renders debt state and channel-correct recovery without private card details", () => {
  const walletWithDebt: WalletResponse = {
    available: 1350,
    daily_free: {
      available: 10,
      granted: 10,
      resets_at: "2026-11-15T00:00:00Z",
      timezone: "UTC",
    },
    purchased: 1350,
    subscription: 0,
    reserved: 0,
    source_debt: 200,
    refund_channel: "stripe",
    recent_activity: [
      {
        id: 1,
        date: "Nov 14",
        activity: "Refund · re_test_1",
        detail: "Source attribution refund",
        source: "Purchased",
        amount_display: "−200",
        amount: -200,
        kind: "refund",
      },
    ],
  };

  render(<WalletView wallet={walletWithDebt} trackView={false} />);

  // Debt banner must be visible with alert role
  const alert = screen.getByRole("alert");
  expect(alert).toHaveTextContent(/source debt/i);
  expect(alert).toHaveTextContent("200");
  expect(alert).toHaveTextContent(/paid ai features paused/i);

  // Daily free credits remain available
  expect(screen.getByLabelText("Daily free credits")).toBeVisible();
  expect(screen.getByText(/10 \/ 10/)).toBeVisible();

  // Channel-correct refund policy/support without private payment details
  expect(screen.getByText(/refund policy|support@duolify.com/i)).toBeVisible();
  expect(screen.queryByText(/\b\d{16}\b/)).toBeNull();

  // Refund row in recent activity
  expect(screen.getByText("Refund · re_test_1")).toBeVisible();
  expect(screen.getByText("−200")).toBeVisible();
});

it("shows store-specific guidance on native mobile without external links", () => {
  const iosWallet: WalletResponse = {
    available: 0,
    daily_free: {
      available: 0,
      granted: 10,
      resets_at: "2026-11-15T00:00:00Z",
      timezone: "UTC",
    },
    purchased: 0,
    subscription: 0,
    reserved: 0,
    source_debt: 400,
    refund_channel: "apple",
    recent_activity: [],
  };

  render(<WalletView wallet={iosWallet} trackView={false} />);
  expect(screen.getByText(/apple app store/i)).toBeVisible();
});
