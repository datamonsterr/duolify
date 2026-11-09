import { useEffect, useState } from "react";
import { Button } from "../design/Button";
import { session } from "../accounts/session";
import { secureStore } from "../accounts/secure-store";
import { quoteAPI } from "../pricing/quotes";
import type { ConfirmedOutline } from "./outline";

type Terms = {
  id: string;
  content_ceiling: number;
  audio_ceiling: number;
  pricing_version: string;
  expires_at: string;
};
type Creation = { id: string; state: string; course_id: string };
interface CreationService {
  quote(snapshot: ConfirmedOutline): Promise<Terms>;
  wallet(): Promise<{
    purchased: number;
    subscription: number;
    unverified?: boolean;
  }>;
  accept(id: string): Promise<unknown>;
  create(snapshot: ConfirmedOutline, quoteID: string): Promise<Creation>;
  get(id: string): Promise<Creation>;
  watch(
    id: string,
    onStage: (stage: Creation) => void,
    signal: AbortSignal,
  ): Promise<void>;
}
const input = (s: ConfirmedOutline) => ({
  draft_id: s.draft_id,
  expected_revision: s.revision,
});
const creationAPI: CreationService = {
  quote: (s) => session.request("/v1/course-creations/quote", "POST", input(s)),
  wallet: () => session.request("/v1/me/credits"),
  accept: quoteAPI.accept,
  create: (s, quoteID) =>
    session.request("/v1/course-creations", "POST", {
      ...input(s),
      quote_id: quoteID,
    }),
  get: (id) =>
    session.request(`/v1/course-creations/${encodeURIComponent(id)}`),
  async watch(id, onStage, signal) {
    const tokens = await secureStore.read();
    const response = await fetch(
      `${import.meta.env.VITE_API_URL || "http://localhost:8080"}/v1/course-creations/${encodeURIComponent(id)}/events`,
      {
        headers: { Authorization: `Bearer ${tokens?.access_token ?? ""}` },
        signal,
      },
    );
    if (!response.ok || !response.body) throw new Error("Status unavailable");
    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let buffer = "";
    try {
      while (true) {
        const { value, done } = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, { stream: true });
        let end;
        while ((end = buffer.indexOf("\n\n")) >= 0) {
          const event = buffer.slice(0, end);
          buffer = buffer.slice(end + 2);
          const data = event
            .split("\n")
            .find((line) => line.startsWith("data: "));
          if (data) onStage(JSON.parse(data.slice(6)) as Creation);
        }
      }
    } finally {
      await reader.cancel();
    }
  },
};
export function CourseCreate({
  snapshot,
  onBack,
  service = creationAPI,
  onEnrolled = (id) => {
    window.location.assign(`/course/${encodeURIComponent(id)}`);
  },
}: {
  snapshot: ConfirmedOutline;
  onBack: () => void;
  service?: CreationService;
  onEnrolled?: (id: string) => void;
}) {
  const key = `course-creation:${session.current()?.id ?? "anonymous"}:${snapshot.draft_id}:${snapshot.revision}`;
  const [terms, setTerms] = useState<Terms>();
  const [balance, setBalance] = useState<number>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [job, setJob] = useState<Creation>();
  const [recovery, setRecovery] = useState(() => sessionStorage.getItem(key));
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, []);
  useEffect(() => {
    if (!job || job.state === "failed" || job.state === "enrolled") return;
    const controller = new AbortController();
    void service.watch(job.id, setJob, controller.signal).catch(() => {
      if (!controller.signal.aborted)
        setError(
          "Connection lost. Your edits are kept. Resume status to reconnect to this job.",
        );
    });
    return () => controller.abort();
  }, [job, service]);
  useEffect(() => {
    if (job?.state === "enrolled" && job.course_id) {
      sessionStorage.removeItem(key);
      onEnrolled(job.course_id);
    }
  }, [job, key, onEnrolled]);
  const expired = !!terms && new Date(terms.expires_at).getTime() <= now;
  async function review() {
    setBusy(true);
    setError("");
    try {
      const [q, w] = await Promise.all([
        service.quote(snapshot),
        service.wallet(),
      ]);
      setTerms(q);
      setBalance(w.unverified ? undefined : w.purchased + w.subscription);
    } catch {
      setError("Cost unavailable. Your edits are kept; connect and retry.");
    } finally {
      setBusy(false);
    }
  }
  async function create() {
    if (
      !terms ||
      expired ||
      busy ||
      balance === undefined ||
      balance < terms.content_ceiling
    )
      return;
    setBusy(true);
    setError("");
    try {
      await service.accept(terms.id);
      // Keep the quote on ambiguous network failure; resubmit only the same immutable intent.
      sessionStorage.setItem(key, `quote:${terms.id}`);
      setRecovery(`quote:${terms.id}`);
      const next = await service.create(snapshot, terms.id);
      sessionStorage.setItem(key, `job:${next.id}`);
      setRecovery(`job:${next.id}`);
      setJob(next);
    } catch {
      setError(
        "Creation unavailable. Your edits are kept. Resume status before trying again.",
      );
    } finally {
      setBusy(false);
    }
  }
  async function resume() {
    if (!recovery || busy) return;
    setBusy(true);
    setError("");
    try {
      const next = recovery.startsWith("job:")
        ? await service.get(recovery.slice(4))
        : await service.create(snapshot, recovery.slice(6));
      sessionStorage.setItem(key, `job:${next.id}`);
      setRecovery(`job:${next.id}`);
      setJob(next);
    } catch {
      setError(
        "Status unavailable. Your edits are kept; connect and resume status.",
      );
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="course-create" aria-label="Create course outline">
      <h2>Create your course outline</h2>
      <p role="status">
        Outline confirmed. No course has been created. Creation needs a separate
        review and confirmation.
      </p>
      <p>
        Confirmed revision {snapshot.revision} · {snapshot.outline.length} units
      </p>
      <p>
        This creates the outline and enrolls you. Lessons are prepared
        separately as you reach them.
      </p>
      {error && <p role="alert">{error}</p>}
      {busy && (
        <p role="status">Preparing your outline… Your edits are kept.</p>
      )}
      {job && (
        <p role="status">
          {job.state === "failed"
            ? "Creation failed. Undelivered reserved credits are released. Your edits are kept."
            : job.state === "enrolled"
              ? "Course ready. Opening your path…"
              : job.state === "validated"
                ? "Saving your outline…"
                : "Preparing your outline…"}
        </p>
      )}
      {terms && !job && (
        <div aria-live="polite">
          <h3>Reserve up to {terms.content_ceiling} credits</h3>
          <p>
            Content up to {terms.content_ceiling} · Audio{" "}
            {terms.audio_ceiling ?? 0} credits. Narration is included in later
            lesson estimates.
          </p>
          <p>
            Daily free 0 → purchased/subscription {terms.content_ceiling}; held
            only after Create. Original terms {terms.pricing_version}.
          </p>
          <p>
            Eligible paid balance {balance ?? "unavailable"}
            {balance !== undefined &&
              ` → ${Math.max(0, balance - terms.content_ceiling)}`}
            . Unused reserved credits return after settlement.
          </p>
          <p>Expires {new Date(terms.expires_at).toLocaleString()}</p>
          {expired && (
            <p role="alert">Quote expired. Review a fresh creation cost.</p>
          )}
          {balance !== undefined && balance < terms.content_ceiling && (
            <p role="alert">
              Not enough eligible credits. Your edits are kept.
            </p>
          )}
          <Button
            disabled={
              busy ||
              expired ||
              !!recovery ||
              balance === undefined ||
              balance < terms.content_ceiling
            }
            onClick={() => void create()}
          >
            Create outline · up to {terms.content_ceiling} credits
          </Button>
        </div>
      )}
      {!recovery && (
        <Button disabled={busy} onClick={() => void review()}>
          Review creation cost
        </Button>
      )}
      {recovery && job?.state !== "failed" && (
        <Button disabled={busy} onClick={() => void resume()}>
          Resume status
        </Button>
      )}
      {(error || terms) && (
        <a href="/credits">Get credits — your edits are kept</a>
      )}
      <Button
        variant="quiet"
        disabled={busy || (!!job && job.state !== "failed")}
        onClick={onBack}
      >
        Back to outline
      </Button>
    </section>
  );
}
