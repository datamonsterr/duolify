import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { session } from "../accounts/session";
import { trackNarrationPlayed } from "../../lib/analytics";
import {
  failedSegmentIndexes,
  locateSegment,
  pausedForCredits,
  pendingSegments,
  playableSegments,
  segmentSlideId,
  type SegmentPlay,
} from "../characters/narration";
import type { ManifestSlide } from "../player/manifest";
import { parseAlignment, spokenCount, type Alignment } from "./alignment";
import { fetchNarrationAssets } from "./cache";

type State = "idle" | "loading" | "playing" | "paused" | "unavailable";

/**
 * E17-08: explicit narration for a slide whose dialogue is spoken by pinned speakers. It reuses the
 * E14 clock, mute, rate, seek/replay, offline cache and alignment checks; each segment plays in its own
 * allowlisted voice in speaker order, and any segment without audio stays readable as a caption.
 */
export function SegmentNarration({
  lessonId,
  slide,
  rate,
  muted,
  captions,
  masterPlaying,
  masterTime,
  onPlaySync,
  onPauseSync,
  onEndedSync,
  onRecovered,
}: {
  lessonId: string;
  slide: ManifestSlide;
  rate: number;
  muted: boolean;
  captions: boolean;
  masterPlaying?: boolean;
  masterTime?: number;
  onPlaySync?: () => void;
  onPauseSync?: () => void;
  onEndedSync?: () => void;
  onRecovered?: () => void;
}) {
  const plays = useMemo(() => playableSegments(slide), [slide]);
  const [state, setState] = useState<State>("idle");
  const [active, setActive] = useState(-1);
  const [alignments, setAlignments] = useState<Record<number, Alignment>>({});
  const [elapsed, setElapsed] = useState(0);
  const [recovery, setRecovery] = useState<"idle" | "working" | "offline">(
    "idle",
  );
  const audio = useRef<HTMLAudioElement | null>(null);
  const url = useRef<string | null>(null);
  const frame = useRef(0);
  const current = useRef<SegmentPlay | null>(null);
  const speaking = useRef(false);
  const prefs = useRef({ rate, muted });
  const startRef = useRef<
    | ((play: SegmentPlay, within: number, announce: boolean) => Promise<void>)
    | null
  >(null);
  const [activeOffset, setActiveOffset] = useState(0);
  useEffect(() => {
    prefs.current = { rate, muted };
  }, [rate, muted]);

  const stopTick = useCallback(() => cancelAnimationFrame(frame.current), []);
  const startTick = useCallback(() => {
    cancelAnimationFrame(frame.current);
    const loop = () => {
      const play = current.current;
      if (audio.current && play)
        setElapsed(play.offset_ms + audio.current.currentTime * 1000);
      frame.current = requestAnimationFrame(loop);
    };
    frame.current = requestAnimationFrame(loop);
  }, []);
  const release = useCallback(() => {
    cancelAnimationFrame(frame.current);
    audio.current?.pause();
    audio.current = null;
    if (url.current) URL.revokeObjectURL(url.current);
    url.current = null;
  }, []);
  useEffect(() => release, [release]);
  useEffect(() => {
    if (!audio.current) return;
    audio.current.playbackRate = rate;
    audio.current.muted = muted;
  }, [rate, muted]);

  // Load, validate and start one segment at a position inside its audio.
  const start = useCallback(
    async (play: SegmentPlay, within: number, announce: boolean) => {
      setState("loading");
      try {
        const assets = await fetchNarrationAssets(
          play.audio_url,
          play.alignment_url,
        );
        const parsed = parseAlignment(assets.rawAlignment, play.segment.text);
        if (!parsed || assets.audioBlob.size === 0)
          throw new Error("invalid narration");
        release();
        url.current = URL.createObjectURL(assets.audioBlob);
        const el = new Audio(url.current);
        el.playbackRate = prefs.current.rate;
        el.muted = prefs.current.muted;
        el.addEventListener("ended", () => {
          stopTick();
          const next = plays.find((p) => p.index > play.index);
          if (next && speaking.current) void startRef.current?.(next, 0, false);
          else {
            speaking.current = false;
            setElapsed(play.offset_ms + play.duration_ms);
            setState("paused");
            onEndedSync?.();
          }
        });
        audio.current = el;
        current.current = play;
        setAlignments((a) => ({ ...a, [play.index]: parsed }));
        setActive(play.index);
        setActiveOffset(play.offset_ms);
        el.currentTime = within / 1000;
        speaking.current = true;
        await el.play();
        setState("playing");
        startTick();
        if (announce) {
          onPlaySync?.();
          trackNarrationPlayed({
            lesson_id: lessonId,
            slide_id: slide.slide_id,
            duration_ms: plays.reduce((n, p) => n + p.duration_ms, 0),
          });
        }
      } catch {
        release();
        speaking.current = false;
        setState("unavailable");
      }
    },
    [
      lessonId,
      onEndedSync,
      onPlaySync,
      plays,
      release,
      slide.slide_id,
      startTick,
      stopTick,
    ],
  );
  useEffect(() => {
    startRef.current = start;
  }, [start]);

  // Follow the parent clock: pause/resume, and seeks or replays move to the segment at that time.
  useEffect(() => {
    if (masterPlaying === undefined || !audio.current) return;
    if (masterPlaying && audio.current.paused) {
      speaking.current = true;
      void audio.current
        .play()
        .then(() => {
          setState("playing");
          startTick();
        })
        .catch(() => {});
    } else if (!masterPlaying && !audio.current.paused) {
      stopTick();
      audio.current.pause();
      setState("paused");
    }
  }, [masterPlaying, startTick, stopTick]);
  useEffect(() => {
    if (masterTime === undefined || !audio.current || !current.current) return;
    const now = current.current.offset_ms + audio.current.currentTime * 1000;
    if (Math.abs(now - masterTime) <= 300) return;
    const target = locateSegment(plays, masterTime);
    if (!target) return;
    if (target.play.index === current.current.index) {
      audio.current.currentTime = target.within_ms / 1000;
      setElapsed(masterTime);
    } else if (speaking.current || !audio.current.paused)
      void start(target.play, target.within_ms, false);
  }, [masterTime, plays, start]);

  async function recover() {
    setRecovery("working");
    try {
      for (const i of failedSegmentIndexes(slide))
        await session.request(
          `/v1/lessons/${lessonId}/slides/${segmentSlideId(slide.slide_id, i)}/narration/retry`,
          "POST",
        );
      setRecovery("idle");
      onRecovered?.();
    } catch {
      setRecovery("offline");
    }
  }

  const segments = slide.segments ?? [];
  const failed = failedSegmentIndexes(slide).length > 0;
  const lines = captions && (
    <ol className="narration-segments" aria-label="Narrated dialogue">
      {segments.map((segment, i) => {
        const a = alignments[i];
        const on = state !== "idle" && state !== "loading" && active === i && a;
        const spoken = on ? spokenCount(a, elapsed - activeOffset) : 0;
        const chars = [...segment.text];
        return (
          <li
            key={i}
            className="narration-segment"
            data-speaker={segment.speaker_key}
            data-voice={segment.narration?.voice_key ?? ""}
            data-state={segment.narration?.status ?? "captions"}
            aria-current={on ? "true" : undefined}
          >
            <strong>{segment.speaker_label}</strong>{" "}
            <span className="narration-caption">
              {on && spoken > 0 ? (
                <>
                  <mark>{chars.slice(0, spoken).join("")}</mark>
                  {chars.slice(spoken).join("")}
                </>
              ) : (
                segment.text
              )}
            </span>
          </li>
        );
      })}
    </ol>
  );

  if (pausedForCredits(slide))
    return (
      <div className="narration">
        <div role="status">
          <p className="narration-title">Narration paused</p>
          <p className="narration-hint">
            Not enough audio credits. The lesson text, captions and test still
            work.
          </p>
          {recovery === "offline" && (
            <p className="narration-hint">
              Could not reach the server. Your place is saved; try again.
            </p>
          )}
        </div>
        <a className="duo-button quiet narration-recover" href="/credits">
          Add credits
        </a>
        {lines}
      </div>
    );
  if (plays.length === 0 && pendingSegments(slide))
    return (
      <div className="narration">
        <p className="narration-hint" role="status">
          Narration is being prepared.
        </p>
        {lines}
      </div>
    );
  if (plays.length === 0 || state === "unavailable")
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
        {failed && (
          <button
            type="button"
            className="duo-button quiet narration-recover"
            disabled={recovery === "working"}
            onClick={() => void recover()}
          >
            Retry narration
          </button>
        )}
        {lines}
      </div>
    );
  const running = state === "playing";
  return (
    <div className="narration">
      {!running && state !== "paused" && (
        <p className="narration-hint" role="status">
          {state === "loading"
            ? "Loading narration…"
            : "Narration is ready. Press Play when you are."}
        </p>
      )}
      {failed && (
        <p className="narration-hint" role="status">
          Some lines have captions only.
        </p>
      )}
      <button
        type="button"
        className="duo-button quiet narration-play"
        disabled={state === "loading"}
        onClick={() => {
          if (running) {
            stopTick();
            audio.current?.pause();
            speaking.current = false;
            setState("paused");
            onPauseSync?.();
          } else if (audio.current && current.current) {
            speaking.current = true;
            void audio.current.play().then(() => {
              setState("playing");
              startTick();
              onPlaySync?.();
            });
          } else void start(plays[0], 0, true);
        }}
      >
        {running ? "Pause narration" : "Play narration"}
      </button>
      {failed && (
        <button
          type="button"
          className="duo-button quiet narration-recover"
          disabled={recovery === "working"}
          onClick={() => void recover()}
        >
          Retry narration
        </button>
      )}
      {lines}
    </div>
  );
}
