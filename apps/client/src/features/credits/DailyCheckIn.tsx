import { useEffect, useState, useSyncExternalStore } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { session } from "../accounts/session";
import { Feedback } from "../design/chat/Feedback";
import { useUiPreferences } from "../design/localization/Preferences";
import "./check-in.css";

export type CheckInResult = {
  granted: number;
  first_time: boolean;
  balance: number;
  banked_checkin: number;
  cap: number;
  next_check_in_at: string;
  checked_in: boolean;
};

const inFlight = new Map<string, Promise<CheckInResult>>();
function checkIn(owner: string) {
  const existing = inFlight.get(owner);
  if (existing) return existing;
  const pending = session.request<CheckInResult>("/v1/me/check-in", "POST");
  inFlight.set(owner, pending);
  void pending
    .finally(() => {
      if (inFlight.get(owner) === pending) inFlight.delete(owner);
    })
    .catch(() => {});
  return pending;
}

// Attendance has its own lifecycle, independent of navigation and wallet reads.
export function DailyCheckIn() {
  useSyncExternalStore(session.subscribe, session.snapshot);
  const owner = session.current()?.id;
  const signedIn = Boolean(owner && !session.expired());
  const queryClient = useQueryClient();
  const { t, number } = useUiPreferences();
  const [notice, setNotice] = useState<CheckInResult | null>(null);
  useEffect(() => {
    if (!signedIn || !owner) return;
    let active = true;
    let pending = false;
    let timer: ReturnType<typeof setTimeout>;
    let dismiss: ReturnType<typeof setTimeout>;
    let nextAt = 0;
    function schedule(at: number) {
      clearTimeout(timer);
      timer = setTimeout(
        () => void check(),
        Math.max(1000, Math.min(2_147_000_000, at - Date.now())),
      );
    }
    async function check() {
      if (!active || pending || !navigator.onLine || session.expired()) return;
      pending = true;
      try {
        const result = await checkIn(owner);
        if (!active) return;
        nextAt = Date.parse(result.next_check_in_at);
        schedule(Number.isFinite(nextAt) ? nextAt : Date.now() + 60_000);
        if (result.checked_in) {
          setNotice(result);
          clearTimeout(dismiss);
          dismiss = setTimeout(() => setNotice(null), 8000);
          void queryClient.invalidateQueries({ queryKey: ["wallet"] });
        }
      } catch {
        // A transient failure never blocks Learn. Retrying remains idempotent.
        schedule(Date.now() + 60_000);
      } finally {
        pending = false;
      }
    }
    const online = () => void check();
    const visible = () => {
      if (document.visibilityState === "visible" && Date.now() >= nextAt)
        void check();
    };
    void check();
    window.addEventListener("online", online);
    window.addEventListener("focus", visible);
    document.addEventListener("visibilitychange", visible);
    return () => {
      active = false;
      clearTimeout(timer);
      clearTimeout(dismiss);
      window.removeEventListener("online", online);
      window.removeEventListener("focus", visible);
      document.removeEventListener("visibilitychange", visible);
    };
  }, [owner, signedIn, queryClient]);
  if (!signedIn || !notice) return null;
  const values = {
    amount: number(notice.granted),
    balance: number(notice.balance),
    cap: number(notice.cap),
  };
  return (
    <div className="check-in-notice">
      <Feedback
        tone="success"
        title={t(notice.granted ? "checkin.granted" : "checkin.full", values)}
      >
        {t(
          notice.granted && notice.banked_checkin >= notice.cap
            ? "checkin.capped"
            : "checkin.balance",
          values,
        )}
      </Feedback>
    </div>
  );
}
