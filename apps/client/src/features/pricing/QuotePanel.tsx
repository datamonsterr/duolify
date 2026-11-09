import { useEffect, useState } from "react";
import { Button } from "../design/Button";
import { exposePricing } from "../../lib/analytics";
import { quoteAPI, type ActionQuote } from "./quotes";
export function QuotePanel({
  narrationCharacters,
}: {
  narrationCharacters?: number;
}) {
  const [quote, setQuote] = useState<ActionQuote>();
  const [action, setAction] = useState("lesson_content");
  const [audio, setAudio] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(false);
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, []);
  const expired =
    !!quote && (quote.expired || Date.parse(quote.expires_at) <= now);
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
  async function accept() {
    if (!quote || expired) return;
    setBusy(true);
    setError(false);
    try {
      await quoteAPI.accept(quote.id);
      setQuote({ ...quote, accepted: true });
    } catch {
      setError(true);
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="pricing-card quote-panel" aria-label="Action quote">
      <h2>{quote ? "Review quote" : "Quote an action"}</h2>
      <p>
        Review separate ceilings before consenting. Accepting terms does not
        reserve credits or start generation.
      </p>
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
      {action === "lesson_content" && (
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
      {!quote && !busy && (
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
          {quote.accepted ? (
            <p role="status">Terms accepted · original version retained.</p>
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
      {quote && (
        <Button
          variant="quiet"
          disabled={busy}
          onClick={() => {
            setQuote(undefined);
            setError(false);
          }}
        >
          Not now
        </Button>
      )}
    </section>
  );
}
