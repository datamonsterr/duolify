import { useEffect, useRef, useState } from "react";
import { session } from "../accounts/session";
import { Button } from "../design/Button";
import { ProposalDiff, type OutlineChange } from "../design/chat/ProposalDiff";
import { quoteAPI, type ActionQuote } from "../pricing/quotes";
import { trackGuidedUi } from "../../lib/analytics";
import type { OutlineDraft } from "./outline";

type Operation = {
  kind: OutlineChange["kind"];
  id: string;
  parent_id?: string;
  position?: number;
  title?: string;
  goal?: string;
};
export type Proposal = {
  id: string;
  draft_id: string;
  base_revision: number;
  base_outline?: OutlineDraft["outline"];
  request?: string;
  state: string;
  decision: string;
  operations: Operation[];
};
type Quote = Pick<
  ActionQuote,
  | "id"
  | "content_ceiling"
  | "pricing_version"
  | "daily_free_eligible"
  | "expires_at"
>;
export interface ProposalService {
  quote(draft: OutlineDraft, request: string): Promise<Quote>;
  accept(id: string): Promise<unknown>;
  generate(
    draft: OutlineDraft,
    request: string,
    quoteId: string,
  ): Promise<Proposal>;
  get(id: string, quoteId?: string): Promise<Proposal>;
  decide(
    id: string,
    action: "apply" | "discard" | "undo",
  ): Promise<{ proposal: Proposal; draft: OutlineDraft }>;
}
const input = (draft: OutlineDraft, request: string) => ({
  draft_id: draft.id,
  expected_revision: draft.revision,
  request,
});
const api: ProposalService = {
  quote: (draft, request) =>
    session.request(
      "/v1/outline-proposals/quote",
      "POST",
      input(draft, request),
    ),
  accept: quoteAPI.accept,
  generate: (draft, request, quoteId) =>
    session.request("/v1/outline-proposals", "POST", {
      ...input(draft, request),
      quote_id: quoteId,
    }),
  get: (id, quoteId) =>
    session.request(
      `/v1/outline-proposals/${encodeURIComponent(id)}${quoteId ? `?quote_id=${encodeURIComponent(quoteId)}` : ""}`,
    ),
  decide: (id, action) =>
    session.request(
      `/v1/outline-proposals/${encodeURIComponent(id)}/${action}`,
      "POST",
      {},
    ),
};
function recoveryKey(id: string) {
  const owner = session.current()?.id;
  return owner ? `outline-proposal:${owner}:${id}` : undefined;
}
function remember(key: string | undefined, value: unknown) {
  try {
    if (key) sessionStorage.setItem(key, JSON.stringify(value));
  } catch {
    /* The mounted request remains available when storage is full. */
  }
}
function restore(key: string | undefined): {
  request?: string;
  quoteId?: string;
  proposalId?: string;
} {
  try {
    return key ? JSON.parse(sessionStorage.getItem(key) || "{}") : {};
  } catch {
    return {};
  }
}
export function OutlineChat({
  draft,
  dirty,
  onDraft,
  onReload,
  service = api,
}: {
  draft: OutlineDraft;
  dirty: boolean;
  onDraft: (draft: OutlineDraft) => void;
  onReload?: () => Promise<void>;
  service?: ProposalService;
}) {
  const key = recoveryKey(draft.id);
  const [saved] = useState(() => restore(key));
  const [request, setRequest] = useState(saved.request ?? "");
  const [quote, setQuote] = useState<Quote>();
  const [quoteRevision, setQuoteRevision] = useState(0);
  const [proposal, setProposal] = useState<Proposal>();
  const [resumeId, setResumeId] = useState(saved.proposalId ?? "");
  const [resumeQuote, setResumeQuote] = useState(saved.quoteId ?? "");
  const [busy, setBusy] = useState(false);
  const lock = useRef(false);
  const decisionStatus = useRef<HTMLParagraphElement>(null);
  const [error, setError] = useState("");
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, []);
  useEffect(() => {
    if (proposal?.decision) decisionStatus.current?.focus();
  }, [proposal?.decision]);
  const stale =
    !!proposal &&
    proposal.decision === "" &&
    (dirty || proposal.base_revision !== draft.revision);
  const expired =
    !!quote &&
    (Date.parse(quote.expires_at) <= now || quoteRevision !== draft.revision);
  async function run(work: () => Promise<void>) {
    if (lock.current) return;
    lock.current = true;
    setBusy(true);
    setError("");
    try {
      await work();
    } catch (e) {
      setError(
        `${e instanceof Error ? e.message : "Connection unavailable."} Your outline and request are kept.`,
      );
    } finally {
      lock.current = false;
      setBusy(false);
    }
  }
  function received(p: Proposal) {
    setProposal(p);
    setResumeId(p.id);
    if (!request && p.request) setRequest(p.request);
    if (p.state === "failed") {
      setResumeQuote("");
      setError(
        "Couldn't suggest a change. Any reserved credits were released. Review a fresh quote to retry.",
      );
    }
    remember(key, {
      request: request || p.request,
      proposalId: p.id,
      quoteId:
        p.state === "pending" || p.state === "validated"
          ? resumeQuote
          : undefined,
    });
  }
  async function decide(action: "apply" | "discard" | "undo") {
    if (!proposal || (action !== "discard" && dirty)) return;
    await run(async () => {
      const result = await service.decide(proposal.id, action);
      setProposal(result.proposal);
      if (action !== "discard") onDraft(result.draft);
      remember(key, { request, proposalId: proposal.id });
    });
  }
  const names = new Map(
    (proposal?.base_outline ?? draft.outline).flatMap(
      (u) =>
        [[u.id, u.title], ...u.lessons.map((l) => [l.id, l.title])] as [
          string,
          string,
        ][],
    ),
  );
  return (
    <aside className="outline-chat" aria-label="Outline chat">
      <h3>Shape your outline</h3>
      <p>
        Ask for a suggestion, then review its diff. Apply, Discard and Undo are
        free.
      </p>
      <label>
        Ask for an outline change
        <textarea
          maxLength={1000}
          value={request}
          disabled={busy}
          onChange={(e) => {
            setRequest(e.target.value);
            setQuote(undefined);
            remember(key, {
              request: e.target.value,
              proposalId: resumeId,
              quoteId: resumeQuote || undefined,
            });
          }}
        />
      </label>
      {dirty && (
        <p>Save your manual edits before requesting or applying a change.</p>
      )}
      {error && <p role="alert">{error}</p>}
      {stale && (
        <p role="alert">
          Proposal out of date. Your manual edits are kept. Reload the outline
          and review a fresh quote.
        </p>
      )}
      {(stale || error) && onReload && (
        <Button
          variant="quiet"
          disabled={busy || dirty}
          onClick={() => void run(onReload)}
        >
          Reload saved outline
        </Button>
      )}
      {quote && (
        <div aria-live="polite">
          <p>
            Up to {quote.content_ceiling} credits · Pricing version{" "}
            {quote.pricing_version}
          </p>
          <p>
            {quote.daily_free_eligible
              ? "Daily free credits eligible, then purchased credits."
              : "Purchased credits required."}
          </p>
          <p>
            A valid delivered proposal uses credits even if discarded. Manual
            edits remain free.
          </p>
          <p>Expires {new Date(quote.expires_at).toLocaleString()}</p>
          {expired ? (
            <p role="alert">
              Quote expired or outline changed. Review a fresh quote.
            </p>
          ) : (
            <Button
              disabled={busy || dirty}
              onClick={() =>
                void run(async () => {
                  if (expired || dirty) return;
                  await service.accept(quote.id);
                  setResumeQuote(quote.id);
                  setResumeId("");
                  setProposal(undefined);
                  remember(key, { request, quoteId: quote.id });
                  setQuote(undefined);
                  trackGuidedUi({
                    event: "ui_control_activated",
                    surface: "outline",
                    controlId: "outline_change_requested",
                  });
                  received(await service.generate(draft, request, quote.id));
                })
              }
            >
              Confirm &amp; suggest change
            </Button>
          )}
        </div>
      )}
      <Button
        disabled={
          busy || dirty || !request.trim() || (!!resumeQuote && !proposal)
        }
        onClick={() =>
          void run(async () => {
            setQuote(undefined);
            setQuote(await service.quote(draft, request));
            setQuoteRevision(draft.revision);
          })
        }
      >
        {quote || stale || proposal?.state === "failed"
          ? "Review fresh change cost"
          : "Review change cost"}
      </Button>
      {(resumeId || resumeQuote) && (
        <Button
          variant="quiet"
          disabled={busy}
          onClick={() =>
            void run(async () =>
              received(
                await service.get(resumeId, resumeId ? undefined : resumeQuote),
              ),
            )
          }
        >
          Resume proposal
        </Button>
      )}
      {busy && <p role="status">Preparing change… Your outline is kept.</p>}
      {proposal &&
        (proposal.state === "pending" || proposal.state === "validated") && (
          <p role="status">
            Delivery pending. Resume this proposal without resending.
          </p>
        )}
      {proposal?.state === "delivered" && !proposal.decision && (
        <p>
          Based on saved revision {proposal.base_revision}. Your outline changes
          only when you Apply.
        </p>
      )}
      {proposal?.state === "delivered" && !proposal.decision && (
        <ProposalDiff
          proposal={{
            id: proposal.id,
            changes: (proposal.operations ?? []).map((op, index) => ({
              id: `${index}:${op.id}`,
              kind: op.kind,
              description:
                op.kind === "moved"
                  ? `${names.get(op.id) ?? op.id} → position ${(op.position ?? 0) + 1}${op.parent_id ? ` in ${names.get(op.parent_id) ?? op.parent_id}` : ""}`
                  : op.kind === "retitled"
                    ? `${names.get(op.id) ?? op.id} → ${op.title}`
                    : (op.title ?? names.get(op.id) ?? op.id),
            })),
          }}
          pending={busy}
          disableApply={stale}
          outlineControls
          onApply={() => void decide("apply")}
          onDiscard={() => void decide("discard")}
        />
      )}
      {proposal?.decision && (
        <p role="status" tabIndex={-1} ref={decisionStatus}>
          Proposal {proposal.decision}.
        </p>
      )}
      {proposal?.decision === "applied" && (
        <Button
          disabled={busy || dirty}
          onClick={() => {
            trackGuidedUi({
              event: "ui_control_activated",
              surface: "outline",
              controlId: "outline_undo",
            });
            void decide("undo");
          }}
        >
          Undo proposal
        </Button>
      )}
    </aside>
  );
}
