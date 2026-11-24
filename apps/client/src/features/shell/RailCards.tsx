import { useEffect, useState } from "react";
import { Link } from "@tanstack/react-router";
import { session } from "../accounts/session";
import { Badge } from "../design/Badge";
import { useUiPreferences } from "../design/localization/Preferences";
import { trackNavigation } from "../../lib/analytics";
import type { WalletResponse } from "../credits/types";

type Streak = { current_days: number; today_done: boolean };

/**
 * Reads `path` once per navigation (the shell outlives routes), from the same endpoints the
 * Credits page (/v1/me/credits) and the streak panel (/v1/gamification/streak) use.
 */
function useLearnerRead<T>(
  path: string,
  revision: string,
  valid: (data: unknown) => data is T,
) {
  const [state, setState] = useState<{ data?: T; failed: boolean }>({
    failed: false,
  });
  useEffect(() => {
    let active = true;
    session
      .request<unknown>(path)
      .then((data) => {
        if (!valid(data)) throw new Error("unexpected response");
        if (active) setState({ data, failed: false });
      })
      .catch(() => active && setState((s) => ({ data: s.data, failed: true })));
    return () => {
      active = false;
    };
    // `valid` is a module-level guard; it never changes between renders.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [path, revision]);
  return state;
}

const isNumber = (value: unknown) =>
  typeof value === "number" && Number.isFinite(value);
const record = (data: unknown) =>
  typeof data === "object" && data !== null
    ? (data as Record<string, unknown>)
    : {};
function isWallet(data: unknown): data is WalletResponse {
  const wallet = record(data);
  return (
    isNumber(wallet.available) &&
    isNumber(wallet.purchased) &&
    isNumber(record(wallet.daily_free).available)
  );
}
function isStreak(data: unknown): data is Streak {
  const streak = record(data);
  return (
    isNumber(streak.current_days) && typeof streak.today_done === "boolean"
  );
}

export function useWallet(revision: string) {
  return useLearnerRead("/v1/me/credits", revision, isWallet);
}

export function useStreak(revision: string) {
  return useLearnerRead("/v1/gamification/streak", revision, isStreak);
}

/** The available credit total, or the unavailable label while loading or offline. */
export function useBalanceLabel(wallet: WalletResponse | undefined) {
  const { t, number } = useUiPreferences();
  return wallet ? `${number(wallet.available)} credits` : t("shell.balance");
}

export function RailStreakCard({ revision }: { revision: string }) {
  const { t, number } = useUiPreferences();
  const { data } = useLearnerRead(
    "/v1/gamification/streak",
    revision,
    isStreak,
  );
  return (
    <section className="card">
      <h2>{t("nav.streak")}</h2>
      {data ? (
        <>
          <Badge role="streak">
            {`${number(data.current_days)} day streak`}
          </Badge>
          <p>{data.today_done ? "Today is done" : "Today isn't done yet"}</p>
          <progress
            className="support-progress"
            value={Math.min(data.current_days, 7)}
            max={7}
            aria-label="Streak days toward a week"
          />
        </>
      ) : (
        <p className="muted">Streak unavailable</p>
      )}
      <Link
        className="shell-link"
        to="/streak"
        onClick={() => trackNavigation("streak")}
      >
        {t("shell.streak")}
      </Link>
    </section>
  );
}

export function RailCreditsCard({
  wallet,
}: {
  wallet: WalletResponse | undefined;
}) {
  const { number } = useUiPreferences();
  const label = useBalanceLabel(wallet);
  return (
    <section className="card">
      <h2>Credit options</h2>
      <Badge role="credit">{label}</Badge>
      <p>
        {wallet
          ? [
              `Daily free ${number(wallet.daily_free.available)}`,
              typeof wallet.check_in === "number" &&
                `Check-in ${number(wallet.check_in)}`,
              `Purchased ${number(wallet.purchased)}`,
            ]
              .filter(Boolean)
              .join(" · ")
          : "Check your balance and available credits."}
      </p>
      <Link
        className="shell-link"
        to="/credits"
        onClick={() => trackNavigation("credits")}
      >
        View credits
      </Link>
    </section>
  );
}
