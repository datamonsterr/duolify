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
function useLearnerRead<T>(path: string, revision: string) {
  const [state, setState] = useState<{ data?: T; failed: boolean }>({
    failed: false,
  });
  useEffect(() => {
    let active = true;
    session
      .request<T>(path)
      .then((data) => active && setState({ data, failed: false }))
      .catch(() => active && setState((s) => ({ data: s.data, failed: true })));
    return () => {
      active = false;
    };
  }, [path, revision]);
  return state;
}

export function useWallet(revision: string) {
  return useLearnerRead<WalletResponse>("/v1/me/credits", revision);
}

/** "1,234 credits" or the unavailable label while loading or offline. */
export function useBalanceLabel(wallet: WalletResponse | undefined) {
  const { t, number } = useUiPreferences();
  return wallet ? `${number(wallet.available)} credits` : t("shell.balance");
}

export function RailStreakCard({ revision }: { revision: string }) {
  const { t, number } = useUiPreferences();
  const { data } = useLearnerRead<Streak>("/v1/gamification/streak", revision);
  return (
    <section className="card">
      <h2>{t("nav.streak")}</h2>
      {data ? (
        <>
          <Badge role="streak">
            {`${number(data.current_days)} day streak`}
          </Badge>
          <p>{data.today_done ? "Today is done" : "Today isn't done yet"}</p>
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
          ? `Daily free ${number(wallet.daily_free.available)} · Purchased ${number(wallet.purchased)}`
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
