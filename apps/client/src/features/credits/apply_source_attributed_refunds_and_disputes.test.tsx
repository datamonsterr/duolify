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
        activity: "Refund",
        detail: "operator note: customer called about re_test_1",
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
  expect(screen.getByText("Refund")).toBeVisible();
  expect(screen.queryByText(/operator note|re_test_1/)).toBeNull();
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

const base: WalletResponse = {
  available: 5,
  daily_free: {
    available: 10,
    granted: 10,
    resets_at: "2026-11-15T00:00:00Z",
    timezone: "UTC",
  },
  purchased: 5,
  subscription: 0,
  reserved: 0,
  recent_activity: [],
};

it("shows Google Play history guidance for the google channel", () => {
  render(
    <WalletView
      wallet={{ ...base, source_debt: 50, refund_channel: "google" }}
      trackView={false}
    />,
  );
  expect(screen.getByText(/google play purchase history/i)).toBeVisible();
  expect(screen.queryByText(/support@duolify.com/i)).toBeNull();
});

it("shows Apple report-a-problem guidance for the apple channel", () => {
  render(
    <WalletView
      wallet={{ ...base, refund_channel: "apple" }}
      trackView={false}
    />,
  );
  expect(screen.getByText(/reportaproblem\.apple\.com/i)).toBeVisible();
});

it("renders no debt banner at zero or absent debt", () => {
  const { rerender } = render(
    <WalletView wallet={{ ...base, source_debt: 0 }} trackView={false} />,
  );
  expect(screen.queryByRole("alert")).toBeNull();
  rerender(<WalletView wallet={base} trackView={false} />);
  expect(screen.queryByRole("alert")).toBeNull();
});

it("never leaks provider ids or free-text notes in refund or chargeback rows", () => {
  render(
    <WalletView
      wallet={{
        ...base,
        recent_activity: [
          {
            id: 1,
            date: "Nov 14",
            activity: "Refund · re_3Pabc123",
            detail: "dp_1Xyz note from operator",
            source: "Purchased",
            amount_display: "−200",
            amount: -200,
            kind: "refund",
          },
          {
            id: 2,
            date: "Nov 14",
            activity: "Chargeback · dp_1Xyz",
            detail: "free text",
            source: "Purchased",
            amount_display: "−50",
            amount: -50,
            kind: "chargeback",
          },
        ],
      }}
      trackView={false}
    />,
  );
  expect(document.body.textContent).not.toMatch(/\b(re|dp|ch|pi)_\w+/);
  expect(document.body.textContent).not.toMatch(/operator|free text/);
});

it("wraps long refund content and keeps the banner reachable by keyboard order", async () => {
  const long = "Refund ".repeat(40);
  render(
    <WalletView
      wallet={{
        ...base,
        source_debt: 123456789,
        recent_activity: [
          {
            id: 1,
            date: "Nov 14",
            activity: long,
            source: "Purchased " + "x".repeat(80),
            amount_display: "−200",
            amount: -200,
            kind: "refund",
          },
        ],
      }}
      trackView={false}
    />,
  );
  expect(screen.getByRole("alert")).toHaveTextContent("123,456,789");
  const row = screen.getByText("−200").closest(".wallet-activity-row");
  expect(row).not.toBeNull();
  // Banner is not a tab stop; the first focusable control follows it in DOM order.
  const button = screen.getByRole("button", { name: "View credit options" });
  expect(
    screen.getByRole("alert").compareDocumentPosition(button) &
      Node.DOCUMENT_POSITION_FOLLOWING,
  ).toBeTruthy();
  button.focus();
  expect(button).toHaveFocus();
  expect(screen.getByRole("alert")).not.toHaveAttribute("tabindex");
});
