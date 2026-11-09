import { useEffect } from "react";
import type { WalletResponse, ActivityItem } from "./types";
import { trackScreen } from "../../lib/analytics";
import { Button } from "../design/Button";
import "./wallet.css";

export type WalletViewProps = {
  wallet: WalletResponse;
  onViewOptions?: () => void;
  onLoadMore?: () => void;
  hasMore?: boolean;
  trackView?: boolean;
};

export function WalletView({
  wallet,
  onViewOptions,
  onLoadMore,
  hasMore,
  trackView = true,
}: WalletViewProps) {
  useEffect(() => {
    if (trackView) {
      trackScreen("credits");
    }
  }, [trackView]);

  const dailyFree = wallet.daily_free;
  const isExhausted = dailyFree.available === 0 && dailyFree.granted > 0;
  const meterPercent =
    dailyFree.granted > 0
      ? Math.max(
          0,
          Math.min(100, (dailyFree.available / dailyFree.granted) * 100),
        )
      : 0;

  const rawCity = dailyFree.timezone.includes("/")
    ? dailyFree.timezone.split("/")[1].replace(/_/g, " ")
    : dailyFree.timezone;
  const timezoneCity = rawCity === "Ho Chi Minh" ? "Ho Chi Minh City" : rawCity;

  return (
    <div className="wallet-container">
      {wallet.unverified && (
        <div role="alert" className="wallet-banner wallet-banner-warn">
          <div>
            <b className="wallet-banner-title">Balance not verified</b>
            <p>
              Offline · last checked {wallet.last_verified_at || "recently"}.
              Purchases and AI actions need a connection.
            </p>
          </div>
        </div>
      )}

      {/* Top Action */}
      <div className="wallet-top-bar">
        <Button
          variant="secondary"
          className="wallet-btn-options"
          onClick={onViewOptions}
        >
          View credit options
        </Button>
      </div>

      {/* Cards Grid */}
      <div className="wallet-cards-grid">
        {/* Card 1: Available */}
        <div className="wallet-card wallet-card-available">
          <span className="wallet-card-label">Available</span>
          <b className="wallet-card-value">{wallet.available}</b>
          {wallet.reserved > 0 && (
            <span className="wallet-card-subtext">
              {wallet.reserved_note ||
                `${wallet.reserved} reserved for a lesson being prepared`}
            </span>
          )}
        </div>

        {/* Card 2: Daily Free */}
        <div className="wallet-card">
          <div
            style={{
              display: "flex",
              justifyContent: "space-between",
              alignItems: "center",
            }}
          >
            <span className="wallet-card-label">
              {dailyFree.granted} daily free credits
            </span>
            <span className="wallet-card-subtext">
              {dailyFree.available} / {dailyFree.granted}
            </span>
          </div>
          <div className="wallet-meter-container">
            <div
              role="meter"
              aria-label="Daily free credits"
              aria-valuenow={dailyFree.available}
              aria-valuemin={0}
              aria-valuemax={dailyFree.granted}
              className="wallet-meter-fill"
              style={{ width: `${meterPercent}%` }}
            />
          </div>
          <span className="wallet-card-subtext">
            {isExhausted ? "Used for today" : "Resets 00:00 " + timezoneCity}
          </span>
        </div>

        {/* Card 3 (Desktop): Purchased · Plus plan */}
        <div className="wallet-card wallet-desktop-only">
          <span className="wallet-card-label">Purchased · Plus plan</span>
          <b className="wallet-card-value">
            {wallet.purchased} · {wallet.subscription}
          </b>
          <span className="wallet-card-subtext">Never expire</span>
        </div>

        {/* Card 4 (Desktop): Reserved */}
        <div className="wallet-card wallet-desktop-only">
          <span className="wallet-card-label">Reserved</span>
          <b className="wallet-card-value">{wallet.reserved}</b>
          <span className="wallet-card-subtext">
            {wallet.reserved > 0
              ? wallet.reserved_note || "Lesson being prepared"
              : "0 reserved"}
          </span>
        </div>

        {/* Cards 3 & 4 (Mobile): 2-Column Grid */}
        <div className="wallet-mobile-grid wallet-mobile-only">
          <div className="wallet-card">
            <span className="wallet-card-label">Purchased</span>
            <b className="wallet-card-value">{wallet.purchased}</b>
            <span className="wallet-card-subtext">Never expires</span>
          </div>
          <div className="wallet-card">
            <span className="wallet-card-label">Plus plan</span>
            <b className="wallet-card-value">{wallet.subscription}</b>
            <span className="wallet-card-subtext">
              {wallet.subscription > 0
                ? "Renews monthly · Dec 1"
                : "No active plan"}
            </span>
          </div>
        </div>
      </div>

      {/* Helper Guidance Text */}
      <p className="wallet-reset-info">
        Daily free credits reset at 00:00 {timezoneCity}, fund notes and tutor
        chat only (about 3 notes and 5–8 messages), are spent first and don't
        carry over.
      </p>

      {/* Exhausted Recovery Banner */}
      {isExhausted && (
        <div className="wallet-banner wallet-banner-info">
          <div>
            <b className="wallet-banner-title">
              Your notes and lessons stay available
            </b>
            <p>
              New quick notes save without AI; you can enrich them after the
              reset.
            </p>
          </div>
        </div>
      )}

      {/* Offline Reset Awaiting Sync Banner */}
      {wallet.unverified && (
        <div className="wallet-banner wallet-banner-info">
          <div>
            <b className="wallet-banner-title">Reset awaiting sync</b>
            <p>
              Your new daily credits appear after we reconnect and confirm the
              day.
            </p>
          </div>
        </div>
      )}

      {/* Recovery Buttons */}
      {isExhausted && (
        <div style={{ display: "flex", gap: "10px", flexWrap: "wrap" }}>
          <Button variant="secondary" onClick={() => {}}>
            Wait for daily reset
          </Button>
        </div>
      )}

      {/* Recent Activity Card */}
      <div className="wallet-activity-card">
        <h3
          style={{
            margin: "0 0 8px",
            fontSize: "16px",
            fontWeight: "900",
            color: "#2B2B2B",
          }}
        >
          Recent activity
        </h3>

        {wallet.recent_activity.length === 0 ? (
          <div className="wallet-activity-empty">
            <p style={{ margin: "0 0 4px", fontWeight: "700" }}>
              No wallet activity yet.
            </p>
            <p style={{ margin: "0 0 16px", color: "#777", fontSize: "13px" }}>
              Balances and reservations appear here when your wallet is
              connected.
            </p>
            <Button variant="primary" onClick={onViewOptions}>
              See packs and plans
            </Button>
          </div>
        ) : (
          <>
            <div className="wallet-activity-header">
              <span>Date</span>
              <span>Activity</span>
              <span>Source</span>
              <span style={{ textAlign: "right" }}>Amount</span>
            </div>

            {wallet.recent_activity.map((item: ActivityItem) => {
              const amountClass = item.amount_display.includes("held")
                ? "wallet-amount-held"
                : item.amount > 0
                  ? "wallet-amount-positive"
                  : "wallet-amount-negative";

              return (
                <div key={item.id} className="wallet-activity-row">
                  <div>
                    <span>{item.date}</span>
                  </div>
                  <div>
                    <span>{item.activity}</span>
                    {item.detail && (
                      <span
                        className="wallet-activity-meta"
                        style={{ display: "block" }}
                      >
                        {item.detail}
                      </span>
                    )}
                  </div>
                  <div className="wallet-activity-meta">
                    <span>{item.source}</span>
                  </div>
                  <div className={amountClass}>
                    <span>{item.amount_display}</span>
                  </div>
                </div>
              );
            })}
          </>
        )}

        {(hasMore || wallet.unverified) && (
          <div style={{ padding: "12px 0", textAlign: "center" }}>
            <Button
              variant="quiet"
              style={{ color: "#1667AB", fontWeight: 800, fontSize: "13px" }}
              onClick={onLoadMore}
            >
              Load more activity
            </Button>
          </div>
        )}
      </div>
    </div>
  );
}
