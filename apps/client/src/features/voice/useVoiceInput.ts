import { useCallback, useContext, useEffect, useRef, useState } from "react";
import {
  QueryClient,
  QueryClientContext,
  useQuery,
} from "@tanstack/react-query";
import {
  trackVoice,
  type VoiceEntryPoint,
  type VoiceErrorClass,
} from "../../lib/analytics";
import { pricingQueryOptions } from "../pricing/query";
import { capResetsAt, classifyError, transcribe } from "./api";
import {
  browserPlatform,
  type RecorderHandle,
  type VoicePlatform,
} from "./platform";
import {
  durationBucket,
  evaluateRecording,
  idleState,
  step,
  type RecorderEffect,
  type RecorderEvent,
  type RecorderState,
} from "./recorder";

export type VoiceNotice =
  | { kind: "denied" }
  | { kind: "no_microphone" }
  | { kind: "silent" }
  | { kind: "too_short" }
  | { kind: "empty"; retry: boolean }
  | { kind: "error"; cls: VoiceErrorClass; retry: boolean };

export type VoiceBlock =
  | "unavailable"
  | "offline"
  | "daily_cap"
  | "unsupported"
  | "no_lesson"
  | "loading"
  | "denied"
  | undefined;

export type Transcript = { text: string; language: string };

type Options = {
  purpose: "tutor" | "note";
  entryPoint: VoiceEntryPoint;
  lessonId: string | undefined;
  onTranscript: (transcript: Transcript) => void;
  platform?: VoicePlatform;
  /** Test seam. */
  now?: () => number;
};

const TICK_MS = 50;
/** Components rendered without an app QueryClient (tests, previews) never fetch pricing. */
const noClient = new QueryClient();

function useOnline() {
  const [online, setOnline] = useState(
    typeof navigator === "undefined" ? true : navigator.onLine,
  );
  useEffect(() => {
    const on = () => setOnline(true);
    const off = () => setOnline(false);
    window.addEventListener("online", on);
    window.addEventListener("offline", off);
    return () => {
      window.removeEventListener("online", on);
      window.removeEventListener("offline", off);
    };
  }, []);
  return online;
}

/** Push-to-talk capture and transcription for one microphone control. */
export function useVoiceInput({
  purpose,
  entryPoint,
  lessonId,
  onTranscript,
  platform = browserPlatform,
  now = () => performance.now(),
}: Options) {
  const appClient = useContext(QueryClientContext);
  const pricing = useQuery(
    { ...pricingQueryOptions(), enabled: !!appClient },
    appClient ?? noClient,
  );
  const online = useOnline();
  const [phase, setPhase] = useState<"idle" | "recording" | "transcribing">(
    "idle",
  );
  const [elapsedMs, setElapsedMs] = useState(0);
  const [level, setLevel] = useState(0);
  const [toggled, setToggled] = useState(false);
  const [notice, setNotice] = useState<VoiceNotice | null>(null);
  const [denied, setDenied] = useState(false);
  /** Counts recordings the Learner cancelled (slide-off, Escape). */
  const [cancelled, setCancelled] = useState(0);
  const [capped, setCapped] = useState(() => !!capResetsAt());
  const machine = useRef<RecorderState>(idleState);
  const session = useRef<{
    ready: Promise<RecorderHandle | undefined>;
    startedAt: number;
  } | null>(null);
  const kept = useRef<{ blob: Blob; elapsedMs: number } | null>(null);
  const latest = useRef({ onTranscript });
  useEffect(() => {
    latest.current = { onTranscript };
  });

  const phaseRef = useRef(phase);
  useEffect(() => {
    phaseRef.current = phase;
  });
  const available = pricing.data ? !!pricing.data.transcribe : false;
  const block: VoiceBlock = !platform.supported()
    ? "unsupported"
    : !lessonId
      ? "no_lesson"
      : !online
        ? "offline"
        : pricing.isPending && !pricing.data
          ? "loading"
          : !available
            ? "unavailable"
            : capped
              ? "daily_cap"
              : denied
                ? "denied"
                : undefined;

  const emit = useCallback(
    (
      event: "voice_recording_finished" | "voice_recording_cancelled",
      ms: number,
    ) => {
      trackVoice({
        event,
        purpose,
        entry_point: entryPoint,
        duration_bucket: durationBucket(ms),
      });
    },
    [purpose, entryPoint],
  );

  const upload = useCallback(
    async (blob: Blob, ms: number) => {
      if (!lessonId) return;
      setPhase("transcribing");
      kept.current = { blob, elapsedMs: ms };
      try {
        const result = await transcribe({
          blob,
          purpose,
          lessonId,
          durationMs: ms,
        });
        const text = result.text.trim();
        const language = (result.language || "").toLowerCase();
        if (!text) {
          setNotice({ kind: "empty", retry: true });
          return;
        }
        kept.current = null;
        setNotice(null);
        latest.current.onTranscript({ text, language });
      } catch (raw) {
        const error = classifyError(raw);
        const cls: VoiceErrorClass =
          error.cls === "failed" &&
          typeof navigator !== "undefined" &&
          !navigator.onLine
            ? "offline"
            : error.cls;
        if (cls === "daily_cap") {
          setCapped(true);
          kept.current = null;
          setNotice(null);
        } else if (cls === "unavailable") {
          kept.current = null;
          void pricing.refetch();
          setNotice({ kind: "error", cls, retry: false });
        } else {
          const retry =
            cls === "failed" || cls === "rate_limited" || cls === "offline";
          if (!retry) kept.current = null;
          setNotice({ kind: "error", cls, retry });
        }
      } finally {
        setPhase("idle");
      }
    },
    [lessonId, pricing, purpose],
  );

  const run = useCallback(
    async (
      effect: Exclude<RecorderEffect, { type: "start" }>,
      startedAt: number,
    ) => {
      const current = session.current;
      if (!current) return;
      session.current = null;
      setPhase(effect.type === "finish" ? "transcribing" : "idle");
      setElapsedMs(0);
      setLevel(0);
      const handle = await current.ready;
      if (!handle) return;
      if (effect.type === "cancel") {
        handle.cancel();
        emit("voice_recording_cancelled", Math.max(0, now() - startedAt));
        setCancelled((n) => n + 1);
        setPhase("idle");
        return;
      }
      const blob = await handle.stop();
      const verdict = evaluateRecording({
        elapsedMs: effect.elapsedMs,
        bytes: blob.size,
        peak: handle.peak(),
      });
      if (verdict !== "ok") {
        emit("voice_recording_cancelled", effect.elapsedMs);
        setNotice(
          verdict === "too_short"
            ? { kind: "too_short" }
            : verdict === "silent"
              ? { kind: "silent" }
              : { kind: "error", cls: "too_large", retry: false },
        );
        setPhase("idle");
        return;
      }
      emit("voice_recording_finished", effect.elapsedMs);
      await upload(blob, effect.elapsedMs);
    },
    [emit, now, upload],
  );

  const dispatch = useCallback(
    (event: RecorderEvent | { type: "slide_off" } | { type: "escape" }) => {
      const before = machine.current;
      const result = step(before, event as RecorderEvent);
      machine.current = result.state;
      setToggled(
        result.state.phase === "recording" && result.state.mode === "toggle",
      );
      if (result.effect?.type === "start") {
        setNotice(null);
        const startedAt = now();
        const ready = platform.start().then(
          (handle) => handle,
          (error: unknown) => {
            // The Learner denied the microphone (or has none): no recording exists.
            machine.current = idleState;
            session.current = null;
            setPhase("idle");
            const name = (error as { name?: string })?.name;
            if (name === "NotAllowedError" || name === "SecurityError") {
              setDenied(true);
              setNotice({ kind: "denied" });
            } else setNotice({ kind: "no_microphone" });
            return undefined;
          },
        );
        session.current = { ready, startedAt };
        setPhase("recording");
      } else if (result.effect) {
        void run(
          result.effect,
          before.phase === "recording" ? before.startedAt : now(),
        );
      }
    },
    [now, platform, run],
  );

  // Timer, level meter and the 30-second stop while a recording is live.
  useEffect(() => {
    if (phase !== "recording") return;
    const id = setInterval(() => {
      const state = machine.current;
      if (state.phase !== "recording") return;
      setElapsedMs(Math.max(0, now() - state.startedAt));
      void session.current?.ready.then((h) => h && setLevel(h.level()));
      dispatch({ type: "tick", at: now() });
    }, TICK_MS);
    return () => clearInterval(id);
  }, [phase, dispatch, now]);

  // Escape cancels from anywhere while recording.
  useEffect(() => {
    if (phase !== "recording") return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.stopPropagation();
        dispatch({ type: "escape" });
      }
    };
    document.addEventListener("keydown", onKey, true);
    return () => document.removeEventListener("keydown", onKey, true);
  }, [phase, dispatch]);

  // Leaving the screen drops any live recording and the kept audio.
  useEffect(
    () => () => {
      void session.current?.ready.then((h) => h?.cancel());
      session.current = null;
      machine.current = idleState;
      kept.current = null;
    },
    [],
  );

  const retry = useCallback(() => {
    const audio = kept.current;
    if (!audio) return;
    setNotice(null);
    void upload(audio.blob, audio.elapsedMs);
  }, [upload]);

  return {
    phase,
    /** True between two taps (tap-to-toggle); false while a hold is in progress. */
    toggled,
    elapsedMs,
    level,
    notice,
    cancelled,
    block,
    capResets: capped ? capResetsAt() : undefined,
    press: () => {
      // A press while a transcription is running or voice is blocked does nothing.
      if (phaseRef.current === "transcribing" || block) return;
      dispatch({ type: "press", at: now() });
    },
    release: () => dispatch({ type: "release", at: now() }),
    slideOff: () => dispatch({ type: "slide_off" }),
    cancel: () => dispatch({ type: "escape" }),
    retry,
    dismiss: () => {
      kept.current = null;
      setNotice(null);
    },
    /** First-use permission request (after the explanation). Resolves true when granted. */
    async requestPermission(): Promise<boolean> {
      try {
        const handle = await platform.start();
        handle.cancel();
        setDenied(false);
        setNotice(null);
        return true;
      } catch (error) {
        const name = (error as { name?: string })?.name;
        if (name === "NotAllowedError" || name === "SecurityError") {
          setDenied(true);
          setNotice({ kind: "denied" });
        } else setNotice({ kind: "no_microphone" });
        return false;
      }
    },
  };
}
