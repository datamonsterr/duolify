import { useEffect, useRef, useState } from "react";
import { Button } from "../design/Button";
import { session } from "../accounts/session";
import type { ActionQuote } from "../pricing/quotes";

type Quote = Pick<
  ActionQuote,
  "id" | "content_ceiling" | "pricing_version" | "expires_at"
>;
type Diff = {
  kind: string;
  lesson_id?: string;
  unit_title: string;
  before?: string;
  after?: string;
  from_unit_id?: string;
};
type Proposal = {
  id: string;
  state: string;
  outline_version: number;
  expires_at: string;
  diff: Diff[];
};
export type ReplanState = {
  quote?: Quote;
  proposal?: Proposal;
  accepted?: boolean;
};
type ReplanService = {
  quote(courseId: string): Promise<Quote>;
  accept(id: string): Promise<unknown>;
  propose(courseId: string, quoteId: string): Promise<Proposal>;
};
const base = (id: string) => `/v1/courses/${encodeURIComponent(id)}/replan`;
const api: ReplanService = {
  quote: (id) => session.request(base(id) + "/quote", "POST", {}),
  accept: (id) =>
    session.request(`/v1/quotes/${encodeURIComponent(id)}/accept`, "POST", {}),
  propose: (id, quoteId) =>
    session.request(base(id), "POST", { quote_id: quoteId }),
};
export function ReplanPanel({
  courseId,
  service = api,
  onBack,
  initial = {},
  onState,
}: {
  courseId: string;
  service?: ReplanService;
  onBack(): void;
  initial?: ReplanState;
  onState?(state: ReplanState): void;
}) {
  const [state, setState] = useState<ReplanState>(initial);
  const [busy, setBusy] = useState(!initial.quote);
  const [error, setError] = useState("");
  const [quoteError, setQuoteError] = useState(false);
  const [now, setNow] = useState(() => Date.now());
  const heading = useRef<HTMLHeadingElement>(null);
  const update = (next: ReplanState) => {
    setState(next);
    onState?.(next);
  };
  async function loadQuote() {
    setBusy(true);
    setError("");
    setQuoteError(false);
    try {
      update({ quote: await service.quote(courseId) });
    } catch {
      setQuoteError(true);
      setError(
        "Couldn’t load a quote. Re-plan is available when its price is approved. Check your connection and retry.",
      );
    } finally {
      setBusy(false);
    }
  }
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, []);
  useEffect(() => {
    let cancelled = false;
    if (!initial.quote) {
      service
        .quote(courseId)
        .then((q) => {
          if (!cancelled) {
            update({ quote: q });
            setBusy(false);
          }
        })
        .catch(() => {
          if (!cancelled) {
            setQuoteError(true);
            setError(
              "Couldn’t load a quote. Re-plan is available when its price is approved. Check your connection and retry.",
            );
            setBusy(false);
          }
        });
    }
    return () => {
      cancelled = true;
    };
    // A panel instance belongs to one Course. Explicit Retry renews the quote.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  useEffect(() => {
    if (!busy) heading.current?.focus();
  }, [busy]);
  async function propose() {
    if (!state.quote) return;
    setBusy(true);
    setError("");
    let next = state;
    try {
      if (!state.accepted) {
        await service.accept(state.quote.id);
        next = { ...state, accepted: true };
        update(next);
      }
      const proposal = await service.propose(courseId, state.quote.id);
      update({ ...next, proposal });
    } catch {
      setError(
        "Couldn’t prepare a proposal. Check the same request again before requesting a new quote. If no proposal was produced, reserved credits are released.",
      );
    } finally {
      setBusy(false);
    }
  }
  const expired = !!state.quote && Date.parse(state.quote.expires_at) <= now;
  const proposed = state.proposal?.state === "proposed";
  return (
    <section
      className="replan-panel"
      aria-label="Re-plan outline"
      aria-busy={busy}
    >
      <Button variant="secondary" onClick={onBack}>
        Back to instructions
      </Button>
      <h3 ref={heading} tabIndex={-1}>
        {proposed ? "Proposed changes" : "Re-plan remaining outline"}
      </h3>
      <p>Generated and unlocked lessons stay unchanged.</p>
      {state.quote && (
        <aside className="replan-price">
          <strong>Up to {state.quote.content_ceiling} credits</strong>
          <p>
            A successfully prepared proposal is charged even if you discard it.
          </p>
          <small>Pricing version {state.quote.pricing_version}</small>
        </aside>
      )}
      {busy && (
        <p role="status">
          {state.accepted ? "Preparing your proposal…" : "Loading quote…"}
        </p>
      )}
      {error && <p role="alert">{error}</p>}
      {!busy && quoteError && (
        <Button onClick={() => void loadQuote()}>Retry quote</Button>
      )}
      {proposed && (
        <>
          <p className="replan-ready">
            Proposal ready. Your course has not changed.
          </p>
          <p>
            Valid until {new Date(state.proposal!.expires_at).toLocaleString()}.
            Outline version {state.proposal!.outline_version}.
          </p>
          <ol className="replan-diff">
            {state.proposal!.diff.map((d, index) => (
              <li key={index}>
                <small>{d.unit_title}</small>
                <span className={`replan-kind replan-${d.kind}`}>
                  {d.kind.charAt(0).toUpperCase() + d.kind.slice(1)}
                </span>
                {d.before && d.before !== d.after && (
                  <del dir="auto">{d.before}</del>
                )}
                {d.after && <span dir="auto">{d.after}</span>}
                {d.kind === "moved" && (
                  <small>
                    From another position{d.from_unit_id ? " or unit" : ""}
                  </small>
                )}
              </li>
            ))}
          </ol>
        </>
      )}
      {!busy && !proposed && state.quote && (
        <>
          {expired && !state.accepted && (
            <p role="alert">
              Quote expired. Review a fresh quote before continuing.
            </p>
          )}
          {!state.accepted && !expired && (
            <Button onClick={() => void propose()}>
              Accept quote and prepare proposal
            </Button>
          )}
          {state.accepted && (
            <Button onClick={() => void propose()}>Check proposal again</Button>
          )}
          {(expired || error) && (
            <Button variant="secondary" onClick={() => void loadQuote()}>
              Review a new quote
            </Button>
          )}
        </>
      )}
    </section>
  );
}
