import { useEffect, useState, type ReactNode } from "react";
import { Link } from "@tanstack/react-router";
import { session } from "../accounts/session";
import { StatIcon } from "../design/StatIcon";
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

type Progress = { daily_xp: number; daily_goal: number };
function isProgress(data: unknown): data is Progress {
  const progress = record(data);
  return isNumber(progress.daily_xp) && isNumber(progress.daily_goal);
}

const DASH = "\u2013";
function Stat({
  to,
  name,
  label,
  icon,
  className = "",
  children,
}: {
  to: "/streak" | "/credits";
  name: "streak" | "credits";
  label: string;
  icon: "flame" | "bolt" | "gem";
  className?: string;
  children: ReactNode;
}) {
  return (
    <Link
      className={`stat stat-${icon} ${className}`.trim()}
      to={to}
      aria-label={label}
      title={label}
      onClick={() => trackNavigation(name)}
    >
      <StatIcon name={icon} />
      {children}
    </Link>
  );
}

/** Streak, XP today and credits in one compact block; each is a link, icon and number only. */
export function SidebarStats({
  wallet,
  revision,
}: {
  wallet: WalletResponse | undefined;
  revision: string;
}) {
  const { t, number } = useUiPreferences();
  const { data: streak } = useStreak(revision);
  const { data: xp } = useLearnerRead("/v1/gamification", revision, isProgress);
  return (
    <div
      role="group"
      className="sidebar-stats"
      aria-label={t("shell.progress")}
    >
      <Stat
        to="/streak"
        name="streak"
        icon="flame"
        label={
          streak
            ? t("shell.stat_streak", { count: number(streak.current_days) })
            : `${t("nav.streak")} ${DASH}`
        }
      >
        <b>{streak ? number(streak.current_days) : DASH}</b>
      </Stat>
      <Stat
        to="/credits"
        name="credits"
        icon="gem"
        label={
          wallet
            ? t("shell.stat_credits", { count: number(wallet.available) })
            : `${t("nav.credits")} ${DASH}`
        }
      >
        <b>{wallet ? number(wallet.available) : DASH}</b>
      </Stat>
      <Stat
        to="/streak"
        name="streak"
        icon="bolt"
        className="stat-xp"
        label={
          xp
            ? t("shell.stat_xp", {
                done: number(xp.daily_xp),
                goal: number(xp.daily_goal),
              })
            : `${t("shell.stat_xp_none")} ${DASH}`
        }
      >
        <b>{xp ? `${number(xp.daily_xp)} / ${number(xp.daily_goal)}` : DASH}</b>
        <progress
          className="support-progress"
          value={xp ? Math.min(xp.daily_xp, xp.daily_goal) : 0}
          max={xp?.daily_goal || 1}
          aria-hidden="true"
        />
      </Stat>
    </div>
  );
}
