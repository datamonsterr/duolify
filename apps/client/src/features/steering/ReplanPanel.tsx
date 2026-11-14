import { useEffect, useId, useRef, useState } from "react";
import { Button } from "../design/Button";
import { AuthError, session } from "../accounts/session";
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
  decision?: string;
  decided_at?: string;
};
// Server refusals that end this quote: nothing was produced, so credits were released.
const TERMINAL = new Set([
  "quote_invalid",
  "protected_lesson_changed",
  "invalid_proposal",
  "generation_failed",
  "replan_unavailable",
  "fork_confirmation_required",
]);
function reasonOf(error: unknown): string {
  if (!(error instanceof AuthError)) return "";
  const d = error.details;
  if (d && typeof d === "object" && "reason_code" in d)
    return String(d.reason_code);
  return error.status === 402 ? "insufficient_credits" : "";
}
export type ReplanState = {
  quote?: Quote;
  proposal?: Proposal;
  accepted?: boolean;
};
export type ReplanService = {
  quote(courseId: string): Promise<Quote>;
  accept(id: string): Promise<unknown>;
  propose(courseId: string, quoteId: string): Promise<Proposal>;
  confirm(courseId: string, proposalId: string): Promise<Proposal>;
  discard(courseId: string, proposalId: string): Promise<Proposal>;
};
const base = (id: string) => `/v1/courses/${encodeURIComponent(id)}/replan`;
const api: ReplanService = {
  quote: (id) => session.request(base(id) + "/quote", "POST", {}),
  accept: (id) =>
    session.request(`/v1/quotes/${encodeURIComponent(id)}/accept`, "POST", {}),
  propose: (id, quoteId) =>
    session.request(base(id), "POST", { quote_id: quoteId }),
  confirm: (id, proposalId) =>
    session.request(
      `${base(id)}/${encodeURIComponent(proposalId)}/confirm`,
      "POST",
      {},
    ),
  discard: (id, proposalId) =>
    session.request(
      `${base(id)}/${encodeURIComponent(proposalId)}/discard`,
      "POST",
      {},
    ),
};
export function ReplanPanel({
  courseId,
  service = api,
  onBack,
  initial = {},
  onState,
}: {
  courseId: string;
  service?: Partial<ReplanService>;
  onBack(): void;
  initial?: ReplanState;
  onState?(state: ReplanState): void;
}) {
  const s = { ...api, ...service };
  const [state, setState] = useState<ReplanState>(initial);
  const [busy, setBusy] = useState(!initial.quote);
  const [submitting, setSubmitting] = useState("");
  const [error, setError] = useState("");
  const [quoteError, setQuoteError] = useState(false);
  const [reason, setReason] = useState("");
  const [now, setNow] = useState(() => Date.now());
  const [confirmOpen, setConfirmOpen] = useState(false);
  const heading = useRef<HTMLHeadingElement>(null);
  const dialogRef = useRef<HTMLDivElement>(null);
  const applyRef = useRef<HTMLButtonElement>(null);
  const dialogWasOpen = useRef(false);
  const dialogTitleId = useId();

  const update = (next: ReplanState) => {
    setState(next);
    onState?.(next);
  };
  async function loadQuote() {
    setBusy(true);
    setSubmitting("Loading quote…");
    setError("");
    setQuoteError(false);
    setReason("");
    try {
      update({ quote: await s.quote(courseId) });
    } catch {
      setQuoteError(true);
      setError(
        "Couldn’t load a quote. Re-plan is available when its price is approved. Check your connection and retry.",
      );
    } finally {
      setSubmitting("");
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
      s.quote(courseId)
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
    if (!busy && !confirmOpen) heading.current?.focus();
  }, [busy, confirmOpen]);

  useEffect(() => {
    if (confirmOpen) {
      dialogRef.current?.focus();
    } else if (dialogWasOpen.current) {
      // Closing the dialog returns focus to the control that opened it.
      applyRef.current?.focus();
    }
    dialogWasOpen.current = confirmOpen;
  }, [confirmOpen]);

  // A refusal that says the proposal was already decided or expired moves the panel to that state.
  function settleFromCode(code: string): boolean {
    const proposal = state.proposal;
    if (!proposal) return false;
    if (code === "proposal_confirmed" || code === "proposal_discarded") {
      update({
        ...state,
        proposal: {
          ...proposal,
          decision: code === "proposal_confirmed" ? "confirmed" : "discarded",
        },
      });
      return true;
    }
    if (code === "proposal_expired") {
      update({
        ...state,
        proposal: { ...proposal, expires_at: new Date().toISOString() },
      });
      return true;
    }
    return false;
  }

  function handleDialogKeys(event: React.KeyboardEvent) {
    if (event.key === "Escape") {
      event.preventDefault();
      event.stopPropagation();
      setConfirmOpen(false);
    } else if (event.key === "Tab" && dialogRef.current) {
      const items = [
        ...dialogRef.current.querySelectorAll<HTMLElement>(
          "button:not([disabled])",
        ),
      ];
      const first = items[0];
      const last = items[items.length - 1];
      if (!first || !last) return;
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    }
  }

  async function propose() {
    if (!state.quote) return;
    setBusy(true);
    setSubmitting("Preparing your proposal…");
    setError("");
    setReason("");
    let next = state;
    try {
      if (!state.accepted) {
        await s.accept(state.quote.id);
        next = { ...state, accepted: true };
        update(next);
      }
      const proposal = await s.propose(courseId, state.quote.id);
      update({ ...next, proposal });
    } catch (cause) {
      const code = reasonOf(cause);
      setReason(code);
      setError(
        code === "insufficient_credits"
          ? `Not enough credits for this proposal. Nothing was charged. Add credits, then check the same request again.`
          : TERMINAL.has(code)
            ? "No proposal was produced, reserved credits were released and your course is unchanged. Review a new quote to try again."
            : "Couldn’t prepare a proposal. Check the same request again before requesting a new quote. If no proposal was produced, reserved credits are released.",
      );
    } finally {
      setSubmitting("");
      setBusy(false);
    }
  }

  async function confirm() {
    if (!state.proposal) return;
    setBusy(true);
    setSubmitting("Applying outline changes…");
    setError("");
    setReason("");
    setConfirmOpen(false);
    try {
      const updated = await s.confirm(courseId, state.proposal.id);
      update({ ...state, proposal: updated });
    } catch (cause) {
      const code = reasonOf(cause);
      setReason(code);
      if (code === "version_conflict" || code === "stale_proposal") {
        setError(
          "Your outline changed since it was made (a new lesson was prepared). Nothing was changed.",
        );
      } else if (!settleFromCode(code)) {
        setError("Couldn’t apply proposal. Try again later.");
      }
    } finally {
      setSubmitting("");
      setBusy(false);
    }
  }

  async function discard() {
    if (!state.proposal) return;
    setBusy(true);
    setSubmitting("Discarding proposal…");
    setError("");
    setReason("");
    try {
      const updated = await s.discard(courseId, state.proposal.id);
      update({ ...state, proposal: updated });
    } catch (cause) {
      const code = reasonOf(cause);
      setReason(code);
      if (!settleFromCode(code)) {
        setError("Couldn’t discard proposal. Try again later.");
      }
    } finally {
      setSubmitting("");
      setBusy(false);
    }
  }

  const expired = !!state.quote && Date.parse(state.quote.expires_at) <= now;
  const isConfirmed = state.proposal?.decision === "confirmed";
  const isDiscarded = state.proposal?.decision === "discarded";
  const isProposalExpired =
    !!state.proposal && Date.parse(state.proposal.expires_at) <= now;
  const isStale = reason === "version_conflict" || reason === "stale_proposal";
  const proposed = state.proposal?.state === "proposed" && !isDiscarded;

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
        {isConfirmed
          ? "Outline updated"
          : proposed
            ? "Proposed changes"
            : "Re-plan remaining outline"}
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
          {submitting ||
            (state.accepted ? "Preparing your proposal…" : "Loading quote…")}
        </p>
      )}
      {error && !isStale && <p role="alert">{error}</p>}
      {!busy && quoteError && (
        <Button onClick={() => void loadQuote()}>Retry quote</Button>
      )}
      {isConfirmed && (
        <>
          <div className="ban bok" role="status">
            <div>
              <b>Outline updated</b>
              <p>Your outline has been updated with the confirmed changes.</p>
            </div>
          </div>
          <Button onClick={onBack}>Back to learn path</Button>
        </>
      )}
      {isStale && (
        <>
          <div className="ban bwarn" role="alert">
            <div>
              <b>This proposal is out of date</b>
              <p>
                Your outline changed since it was made (a new lesson was
                prepared). Nothing was changed.
              </p>
            </div>
          </div>
          <Button variant="secondary" onClick={() => void loadQuote()}>
            Request a new proposal · up to {state.quote?.content_ceiling}
          </Button>
        </>
      )}
      {isDiscarded && !isStale && (
        <>
          <div className="ban binfo" role="status">
            <div>
              <b>Proposal discarded</b>
              <p>Your course outline is unchanged.</p>
            </div>
          </div>
          <Button variant="secondary" onClick={() => void loadQuote()}>
            Request a new proposal · up to {state.quote?.content_ceiling}
          </Button>
        </>
      )}
      {isProposalExpired && !isConfirmed && !isDiscarded && !isStale && (
        <>
          <div className="ban bwarn" role="alert">
            <div>
              <b>This proposal has expired</b>
              <p>
                Proposals expire after 24 hours. Review a new quote to continue.
              </p>
            </div>
          </div>
          <Button variant="secondary" onClick={() => void loadQuote()}>
            Review a new quote
          </Button>
        </>
      )}
      {proposed && !isConfirmed && !isStale && !isProposalExpired && (
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
          <div className="steering-actions">
            <Button
              variant="secondary"
              onClick={() => void discard()}
              disabled={busy}
            >
              Discard
            </Button>
            <Button
              ref={applyRef}
              onClick={() => setConfirmOpen(true)}
              disabled={busy}
            >
              Apply changes
            </Button>
          </div>
        </>
      )}
      {!busy &&
        !proposed &&
        !isConfirmed &&
        !isDiscarded &&
        !isStale &&
        state.quote && (
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
            {reason === "insufficient_credits" && (
              <a href="/credits">View credit options</a>
            )}
            {state.accepted && !TERMINAL.has(reason) && (
              <Button onClick={() => void propose()}>
                Check proposal again
              </Button>
            )}
            {(expired || error) && (
              <Button variant="secondary" onClick={() => void loadQuote()}>
                Review a new quote
              </Button>
            )}
          </>
        )}
      {confirmOpen && (
        <div className="steering-fork-backdrop">
          <div
            ref={dialogRef}
            className="steering-fork"
            role="dialog"
            aria-modal="true"
            aria-labelledby={dialogTitleId}
            tabIndex={-1}
            onKeyDown={handleDialogKeys}
          >
            <h2 id={dialogTitleId}>Apply outline changes?</h2>
            <p>
              Your remaining outline will be updated. Generated and unlocked
              lessons are never changed.
            </p>
            <div className="steering-actions">
              <Button
                variant="secondary"
                onClick={() => setConfirmOpen(false)}
                disabled={busy}
              >
                Cancel
              </Button>
              <Button onClick={() => void confirm()} disabled={busy}>
                Apply changes
              </Button>
            </div>
          </div>
        </div>
      )}
    </section>
  );
}
