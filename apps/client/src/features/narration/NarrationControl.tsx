import { useEffect, useRef, useState } from "react";
import { session } from "../accounts/session";
import { trackNarrationPlayed } from "../../lib/analytics";
import type { ManifestSlide } from "../player/manifest";
import { parseAlignment, spokenCount, type Alignment } from "./alignment";

type State = "idle" | "loading" | "playing" | "paused" | "unavailable";
type Recovery =
  "idle" | "working" | "restored" | "regenerating" | "pending" | "offline";

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
  onRecovered,
}: {
  lessonId: string;
  slide: ManifestSlide;
  rate: number;
  muted: boolean;
  captions: boolean;
  /** Called after Retry or Restore so the player can reload the manifest's narration status. */
  onRecovered?: () => void;
}) {
  const [state, setState] = useState<State>("idle");
  const [alignment, setAlignment] = useState<Alignment | null>(null);
  const [elapsed, setElapsed] = useState(0);
  const audio = useRef<HTMLAudioElement | null>(null);
  const url = useRef<string | null>(null);
  const frame = useRef(0);
  const [recovery, setRecovery] = useState<Recovery>("idle");
  // Restore is offered only when the object could not be read, never for invalid audio.
  const [unreadable, setUnreadable] = useState(false);

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

  // While a retry or tracked regeneration is open, follow the manifest until new audio is ready.
  const [stale, setStale] = useState<ManifestSlide["narration"]>(undefined);
  const waiting = recovery === "pending" || recovery === "regenerating";
  const settled =
    waiting &&
    slide.narration_status === "ready" &&
    !!slide.narration &&
    slide.narration !== stale;
  const recovered = useRef(onRecovered);
  useEffect(() => {
    recovered.current = onRecovered;
  }, [onRecovered]);
  useEffect(() => {
    if (!waiting || settled) return;
    const timer = setInterval(() => recovered.current?.(), 2000);
    return () => clearInterval(timer);
  }, [waiting, settled]);

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
      let raw: unknown;
      let blob: Blob;
      try {
        [raw, blob] = await Promise.all([
          session.request<unknown>(asset.alignment_url),
          session.blob(asset.audio_url),
        ]);
      } catch (error) {
        setUnreadable(true);
        throw error;
      }
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

  // Retry (failed audio) and Restore (missing object) never charge Credits and send no body.
  async function recover(action: "retry" | "recover") {
    setStale(slide.narration);
    setRecovery("working");
    try {
      const result = await session.request<{ state?: string }>(
        `/v1/lessons/${lessonId}/slides/${slide.slide_id}/narration/${action}`,
        "POST",
      );
      if (result.state === "restored" || result.state === "ok") {
        // Restored audio waits for an explicit Play, like any narration.
        setState("idle");
        setUnreadable(false);
        setRecovery("restored");
      } else if (result.state === "regenerating") setRecovery("regenerating");
      else if (result.state === "pending") setRecovery("pending");
      else setRecovery("idle");
      onRecovered?.();
    } catch {
      setRecovery("offline");
    }
  }

  if (slide.narration_status === "disabled") return null;
  // A refreshed manifest with new ready audio returns to the normal explicit Play control.
  const view = settled ? "idle" : recovery;
  if (view === "regenerating")
    return (
      <div className="narration" role="status">
        <p className="narration-title">Narration unavailable</p>
        <p className="narration-hint">
          Narration is being restored. Captions stay available.
        </p>
      </div>
    );
  if (view === "pending")
    return (
      <div className="narration" role="status">
        <p className="narration-hint">Narration is being prepared.</p>
      </div>
    );
  const unavailable =
    !settled &&
    (slide.narration_status === "failed" || state === "unavailable");
  if (unavailable && view !== "restored")
    return (
      <div className="narration">
        <div role="status">
          <p className="narration-title">Narration unavailable</p>
          <p className="narration-hint">
            Read the captions and transcript, then continue. The animation still
            plays.
          </p>
          {recovery === "offline" && (
            <p className="narration-hint">
              Could not reach the server. Your place is saved; try again.
            </p>
          )}
        </div>
        {(slide.narration_status === "failed" || unreadable) && (
          <button
            type="button"
            className="duo-button quiet narration-recover"
            disabled={recovery === "working"}
            onClick={() =>
              void recover(
                slide.narration_status === "failed" ? "retry" : "recover",
              )
            }
          >
            {slide.narration_status === "failed"
              ? "Retry narration"
              : "Restore narration"}
          </button>
        )}
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
            : recovery === "restored"
              ? "Narration restored. Press Play when you are."
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
