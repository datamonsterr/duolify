import { useEffect, useRef, useState } from "react";
import { session } from "../accounts/session";
import { trackNarrationPlayed } from "../../lib/analytics";
import type { ManifestSlide } from "../player/manifest";
import { parseAlignment, spokenCount, type Alignment } from "./alignment";

type State = "idle" | "loading" | "playing" | "paused" | "unavailable";

/**
 * Explicit narration playback for one slide (S09 "Idle — narration ready"). Audio only
 * starts when the Learner presses Play; captions survive mute and failure.
 */
export function NarrationControl({
  lessonId,
  slide,
  rate,
  muted,
  captions,
}: {
  lessonId: string;
  slide: ManifestSlide;
  rate: number;
  muted: boolean;
  captions: boolean;
}) {
  const [state, setState] = useState<State>("idle");
  const [alignment, setAlignment] = useState<Alignment | null>(null);
  const [elapsed, setElapsed] = useState(0);
  const audio = useRef<HTMLAudioElement | null>(null);
  const url = useRef<string | null>(null);
  const frame = useRef(0);

  const stop = () => {
    cancelAnimationFrame(frame.current);
    audio.current?.pause();
  };
  useEffect(
    () => () => {
      cancelAnimationFrame(frame.current);
      audio.current?.pause();
      audio.current = null;
      if (url.current) URL.revokeObjectURL(url.current);
    },
    [],
  );
  useEffect(() => {
    if (!audio.current) return;
    audio.current.playbackRate = rate;
    audio.current.muted = muted;
  }, [rate, muted]);

  const tick = () => {
    if (audio.current) setElapsed(audio.current.currentTime * 1000);
    frame.current = requestAnimationFrame(tick);
  };

  async function play() {
    if (audio.current && alignment) {
      await audio.current.play();
      setState("playing");
      frame.current = requestAnimationFrame(tick);
      return;
    }
    const asset = slide.narration;
    if (!asset) return;
    setState("loading");
    try {
      const [raw, blob] = await Promise.all([
        session.request<unknown>(asset.alignment_url),
        session.blob(asset.audio_url),
      ]);
      const parsed = parseAlignment(raw, slide.narration_text);
      if (!parsed || blob.size === 0) throw new Error("invalid narration");
      url.current = URL.createObjectURL(blob);
      const el = new Audio(url.current);
      el.playbackRate = rate;
      el.muted = muted;
      el.addEventListener("ended", () => {
        cancelAnimationFrame(frame.current);
        setElapsed(parsed.duration_ms);
        setState("paused");
      });
      audio.current = el;
      setAlignment(parsed);
      await el.play();
      setElapsed(0);
      setState("playing");
      frame.current = requestAnimationFrame(tick);
      trackNarrationPlayed({
        lesson_id: lessonId,
        slide_id: slide.slide_id,
        duration_ms: asset.duration_ms,
      });
    } catch {
      stop();
      audio.current = null;
      setAlignment(null);
      setState("unavailable");
    }
  }

  if (slide.narration_status === "disabled") return null;
  const unavailable =
    slide.narration_status === "failed" || state === "unavailable";
  if (unavailable)
    return (
      <div className="narration" role="status">
        <p className="narration-title">Narration unavailable</p>
        <p className="narration-hint">
          Read the captions and transcript, then continue. The animation still
          plays.
        </p>
      </div>
    );
  if (slide.narration_status === "pending")
    return (
      <div className="narration" role="status">
        <p className="narration-hint">Narration is being prepared.</p>
      </div>
    );
  const active = alignment !== null && state !== "idle" && state !== "loading";
  const spoken = alignment ? spokenCount(alignment, elapsed) : 0;
  const chars = alignment?.characters ?? [];
  return (
    <div className="narration">
      {!active && (
        <p className="narration-hint" role="status">
          {state === "loading"
            ? "Loading narration…"
            : "Narration is ready. Press Play when you are."}
        </p>
      )}
      <button
        type="button"
        className="duo-button quiet narration-play"
        disabled={state === "loading"}
        onClick={() => {
          if (state === "playing") {
            stop();
            setState("paused");
          } else void play();
        }}
      >
        {state === "playing" ? "Pause narration" : "Play narration"}
      </button>
      {active && captions && (
        <p className="narration-caption" aria-live="off">
          {spoken > 0 && <mark>{chars.slice(0, spoken).join("")}</mark>}
          {chars.slice(spoken).join("")}
        </p>
      )}
    </div>
  );
}
