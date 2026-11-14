import { ReservationPanel } from "../credits/ReservationPanel";
import { useEffect, useRef, useState } from "react";
import { Button } from "../design/Button";
import { exposePricing } from "../../lib/analytics";
import { quoteAPI, type ActionQuote } from "./quotes";
export function QuotePanel({
  narrationCharacters,
  recoveryQuoteId,
  onInsufficient,
  onQuoteRecovered,
  consent,
}: {
  narrationCharacters?: number;
  recoveryQuoteId?: string;
  onInsufficient?: (quote: ActionQuote) => void;
  onQuoteRecovered?: (quote: ActionQuote) => void;
  /** Fixed lesson_content consent: quotes on mount, accepting hands the quote to the caller. */
  consent?: { onAccepted: (quote: ActionQuote) => void; onCancel: () => void };
}) {
  const [quote, setQuote] = useState<ActionQuote>();
  const [action, setAction] = useState("lesson_content");
  const [audio, setAudio] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(false);
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!recoveryQuoteId) return;
    let cancelled = false;
    void quoteAPI
      .get(recoveryQuoteId)
      .then((saved) => {
        if (!cancelled) {
          setQuote(saved);
          setAction(saved.action);
          onQuoteRecovered?.(saved);
        }
      })
      .catch(() => {
        if (!cancelled) setError(true);
      });
    return () => {
      cancelled = true;
    };
  }, [recoveryQuoteId, onQuoteRecovered]);
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, []);
  const expired =
    !!quote && (quote.expired || Date.parse(quote.expires_at) <= now);
  const asked = useRef(false);
  async function create() {
    setBusy(true);
    setError(false);
    try {
      const next = await quoteAPI.create(
        action,
        audio && action === "lesson_content" ? narrationCharacters : undefined,
      );
      setQuote(next);
      exposePricing(next.pricing_version, "quote");
    } catch {
      setError(true);
    } finally {
      setBusy(false);
    }
  }
  useEffect(() => {
    if (!consent || asked.current) return;
    asked.current = true;
    void create();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [!!consent]);
  async function accept() {
    if (!quote || expired) return;
    setBusy(true);
    setError(false);
    try {
      await quoteAPI.accept(quote.id);
      setQuote({ ...quote, accepted: true });
      consent?.onAccepted({ ...quote, accepted: true });
    } catch {
      setError(true);
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="pricing-card quote-panel" aria-label="Action quote">
      <h2>
        {consent
          ? "Prepare lesson"
          : quote
            ? "Review quote"
            : "Quote an action"}
      </h2>
      <p>
        Review separate ceilings before consenting. Accepting terms does not
        reserve credits or start generation.
      </p>
      {!consent && (
        <label>
          Action{" "}
          <select
            value={action}
            onChange={(e) => {
              setAction(e.target.value);
              setQuote(undefined);
              setError(false);
            }}
            disabled={busy}
          >
            <option value="lesson_content">Lesson content</option>
            <option value="preview">Course preview</option>
            <option value="outline">Course outline</option>
            <option value="tutor_short">Tutor short</option>
            <option value="tutor_long">Tutor long</option>
            <option value="quick_note">Quick note</option>
          </select>
        </label>
      )}
      {!consent && action === "lesson_content" && (
        <label>
          <input
            type="checkbox"
            checked={audio}
            disabled={busy}
            onChange={(e) => {
              setAudio(e.target.checked);
              setQuote(undefined);
            }}
          />
          Include narration
        </label>
      )}
      {!quote && !busy && !consent && (
        <p>No quote yet. Request current terms to continue.</p>
      )}
      {busy && <p role="status">Loading quote…</p>}
      {error && (
        <p role="alert">
          Quote unavailable or expired. Check your connection, renew quote and
          review terms.
        </p>
      )}
      {quote && (
        <div aria-live="polite">
          <p>Content ceiling: {quote.content_ceiling} credits</p>
          <p>Audio ceiling: {quote.audio_ceiling} credits</p>
          <p>
            {quote.daily_free_eligible
              ? "Daily free credits eligible; spent first."
              : "Purchased credits required; daily free credits are ineligible."}
          </p>
          <p className="pricing-version">
            Original terms · {quote.pricing_version} · {quote.currency}
          </p>
          {quote.bounds && (
            <p>
              Input limit: {quote.bounds.input_tokens} tokens · Output limit:{" "}
              {quote.bounds.output_tokens} tokens
            </p>
          )}
          {quote.rate?.model && <p>Model: {quote.rate.model}</p>}
          {quote.input?.voice && <p>Voice: {quote.input.voice}</p>}
          <p>Expires: {new Date(quote.expires_at).toLocaleString()}</p>
          {quote.accepted && consent ? (
            <p role="status">Terms accepted.</p>
          ) : quote.accepted ? (
            <>
              <p role="status">Terms accepted · original version retained.</p>
              {expired ? (
                <p role="alert">
                  Quote expired. Renew and review before reserving.
                </p>
              ) : (
                <ReservationPanel
                  key={quote.id}
                  quoteId={quote.id}
                  onInsufficient={
                    onInsufficient ? () => onInsufficient(quote) : undefined
                  }
                />
              )}
            </>
          ) : expired ? (
            <p role="alert">
              Quote expired. Renew and review before accepting.
            </p>
          ) : (
            <Button disabled={busy || error} onClick={() => void accept()}>
              Accept quote terms
            </Button>
          )}
        </div>
      )}
      <Button variant="quiet" disabled={busy} onClick={() => void create()}>
        {quote || error ? "Renew quote" : "Get action quote"}
      </Button>
      {(quote || consent) && (
        <Button
          variant="quiet"
          disabled={busy && !consent}
          onClick={() => {
            setQuote(undefined);
            setError(false);
            consent?.onCancel();
          }}
        >
          Not now
        </Button>
      )}
    </section>
  );
}
