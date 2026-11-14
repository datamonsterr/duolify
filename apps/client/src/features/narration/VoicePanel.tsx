import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { trackPurchase } from "../../lib/analytics";
import { pricingQueryOptions } from "../pricing/query";
import { formatCredits } from "../pricing/format";
import {
  voiceAPI,
  voiceErrorCode,
  type VoiceList,
  type VoiceQuote,
} from "./voices";

type Phase =
  | { kind: "idle" }
  | { kind: "quoting"; voice: string }
  | { kind: "quote"; quote: VoiceQuote }
  | { kind: "applying"; quote: VoiceQuote }
  | { kind: "preparing"; voice: string }
  | { kind: "ready"; voice: string }
  | { kind: "declined" }
  | { kind: "failed"; code: string; quote?: VoiceQuote };

/**
 * Voice choice for one Lesson. Choosing a voice only fetches a quote; synthesis starts after the
 * Learner confirms it, and declining changes nothing. The current narration keeps playing.
 */
export function VoicePanel({
  lessonId,
  onChanged,
}: {
  lessonId: string;
  /** Called when the new voice is ready so the player reloads narration status. */
  onChanged?: () => void;
}) {
  const [phase, setPhase] = useState<Phase>({ kind: "idle" });
  const pricing = useQuery(pricingQueryOptions());
  const voices = useQuery<VoiceList>({
    queryKey: ["narration-voices", lessonId],
    queryFn: () => voiceAPI.list(lessonId),
    retry: false,
    staleTime: 0,
  });
  const { refetch } = voices;
  const data = voices.data ?? null;
  const loadFailed = voices.isError && !data;
  const upsellSent = useRef(false);
  const changed = useRef(onChanged);
  useEffect(() => {
    changed.current = onChanged;
  }, [onChanged]);

  // Upgrade guidance is shown only to plans without "No upsell banners"; the API decides.
  const showUpsell =
    !!data && data.narration_enabled && !data.entitled && data.upsell_allowed;
  const pricingVersion = pricing.data?.version;
  useEffect(() => {
    if (!showUpsell || !pricingVersion || upsellSent.current) return;
    upsellSent.current = true;
    trackPurchase({
      event: "credits_upsell_shown",
      pricingVersion,
      feature: "narration_voice",
    });
  }, [showUpsell, pricingVersion]);

  // After consent, follow the chosen voice until it is stored, paused for credits or failed.
  const preparing = phase.kind === "preparing" ? phase.voice : null;
  useEffect(() => {
    if (!preparing) return;
    const timer = setInterval(async () => {
      try {
        const next = (await refetch()).data;
        const change = next?.change;
        if (!change || change.voice_key !== preparing) return;
        if (change.state === "ready") {
          setPhase({ kind: "ready", voice: preparing });
          changed.current?.();
        } else if (change.state === "disabled" || change.state === "failed") {
          setPhase({ kind: "failed", code: change.reason || change.state });
        }
      } catch {
        /* Keep following; the current narration is unaffected. */
      }
    }, 1500);
    return () => clearInterval(timer);
  }, [preparing, refetch]);

  async function choose(voice: string) {
    if (!data || voice === data.selected) return;
    setPhase({ kind: "quoting", voice });
    try {
      setPhase({ kind: "quote", quote: await voiceAPI.quote(lessonId, voice) });
    } catch (error) {
      setPhase({ kind: "failed", code: voiceErrorCode(error).code });
    }
  }
  async function confirm(quote: VoiceQuote) {
    setPhase({ kind: "applying", quote });
    try {
      const result = await voiceAPI.apply(
        lessonId,
        quote.voice_key,
        quote.quote_id,
        quote.config_hash,
      );
      if (result.state === "ready") {
        await voices.refetch();
        setPhase({ kind: "ready", voice: quote.voice_key });
        changed.current?.();
      } else setPhase({ kind: "preparing", voice: quote.voice_key });
    } catch (error) {
      setPhase({ kind: "failed", code: voiceErrorCode(error).code, quote });
    }
  }

  if (loadFailed)
    return (
      <div className="voice-panel">
        <p role="status">
          Voices are unavailable right now. Narration you already have still
          plays.
        </p>
        <button
          type="button"
          className="duo-button quiet voice-action"
          onClick={() => void voices.refetch()}
        >
          Retry voices
        </button>
      </div>
    );
  if (!data) return <p role="status">Loading voices…</p>;
  if (!data.narration_enabled)
    return (
      <div className="voice-panel">
        <p role="status">
          Narration is off, so there is nothing to synthesize. Turn narration on
          in Settings to choose a voice.
        </p>
        <a className="duo-button quiet voice-action" href="/settings">
          Open settings
        </a>
      </div>
    );

  const label = (key: string) =>
    data.voices.find((v) => v.key === key)?.label ?? key;
  const busy =
    phase.kind === "quoting" ||
    phase.kind === "applying" ||
    phase.kind === "preparing";
  return (
    <div className="voice-panel">
      <fieldset className="voice-list" disabled={busy}>
        <legend>Narration voice</legend>
        {data.voices.map((v) => (
          <label
            key={v.key}
            className={`voice-option ${v.available ? "" : "locked"}`}
          >
            <input
              type="radio"
              name="narration-voice"
              value={v.key}
              checked={data.selected === v.key}
              disabled={!v.available}
              onChange={() => void choose(v.key)}
            />
            <span>{v.label}</span>
            {!v.available && (
              <span className="voice-lock">
                {" "}
                · Included with a plan that has curated voices
              </span>
            )}
          </label>
        ))}
      </fieldset>
      {showUpsell && (
        <p className="voice-upsell" role="note">
          Curated voices come with Plus and Pro.{" "}
          <a href="/credits">See plans</a>
        </p>
      )}
      {phase.kind === "quoting" && (
        <p role="status">Checking the cost of {label(phase.voice)}…</p>
      )}
      {phase.kind === "quote" && (
        <section className="voice-quote" aria-label="Voice quote">
          <h3>{label(phase.quote.voice_key)}</h3>
          {phase.quote.free ? (
            <p>
              This voice is already prepared for this lesson. Switching is free.
            </p>
          ) : (
            <p>
              Narrating this lesson in this voice needs {phase.quote.characters}{" "}
              new characters, up to {formatCredits(phase.quote.credits)}{" "}
              credits. Nothing is charged until you confirm. Your current
              narration stays available.
            </p>
          )}
          <div className="voice-actions">
            <button
              type="button"
              className="duo-button primary voice-action"
              onClick={() => void confirm(phase.quote)}
            >
              {phase.quote.free ? "Use this voice" : "Confirm and narrate"}
            </button>
            <button
              type="button"
              className="duo-button quiet voice-action"
              onClick={() => setPhase({ kind: "declined" })}
            >
              Keep current voice
            </button>
          </div>
        </section>
      )}
      {phase.kind === "applying" && <p role="status">Confirming…</p>}
      {phase.kind === "preparing" && (
        <p role="status">
          Preparing {label(phase.voice)}. Your current narration keeps playing.
        </p>
      )}
      {phase.kind === "ready" && (
        <p role="status">
          {label(phase.voice)} is ready. Press Play when you are.
        </p>
      )}
      {phase.kind === "declined" && (
        <p role="status">Kept {label(data.selected)}. Nothing was charged.</p>
      )}
      {phase.kind === "failed" && (
        <Failure
          code={phase.code}
          onReview={() => setPhase({ kind: "idle" })}
        />
      )}
    </div>
  );
}

function Failure({ code, onReview }: { code: string; onReview: () => void }) {
  if (code === "voice_insufficient_credits")
    return (
      <div role="status" className="voice-failure">
        <p>
          Not enough audio credits for this voice. Your current narration, the
          lesson text and the test still work.
        </p>
        <a className="duo-button quiet voice-action" href="/credits">
          Add credits
        </a>
        <button
          type="button"
          className="duo-button quiet voice-action"
          onClick={onReview}
        >
          Review the quote again
        </button>
      </div>
    );
  const text: Record<string, string> = {
    voice_not_entitled: "Curated voices are not part of your plan.",
    narration_off: "Narration is off.",
    quote_changed: "The quote changed. Review the new terms and try again.",
    quote_expired:
      "The quote expired, so nothing was charged. Review it again.",
    voice_unavailable: "That voice is not available.",
  };
  return (
    <div role="status" className="voice-failure">
      <p>
        {text[code] ??
          "Could not prepare that voice. Nothing was charged and your current narration still plays."}
      </p>
      <button
        type="button"
        className="duo-button quiet voice-action"
        onClick={onReview}
      >
        Choose again
      </button>
    </div>
  );
}
