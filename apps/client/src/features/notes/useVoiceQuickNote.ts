import { useCallback, useEffect, useRef, type RefObject } from "react";
import { showToast } from "../../components/toast";
import {
  trackVoiceQuickNote,
  type VoiceQuickNoteSurface,
} from "../../lib/analytics";
import { useUiPreferences } from "../design/localization/Preferences";
import type { Transcript, VoiceNotice } from "../voice/useVoiceInput";
import type { VoiceBubbleControls } from "../voice/VoiceBubble";
import type { NoteAnchor } from "./note-queue";
import { saveNote, undoSavedNote } from "./save-note";

/** What the hook needs to know about the player at the instant it is asked. */
export type QuickNotePlayer = {
  courseId?: string;
  lessonId?: string;
  slideIndex: number;
  slideTitle: string;
  timeMs: number;
  playing: boolean;
  region?: NonNullable<NoteAnchor["rect"]>;
  pause: () => void;
  /** Starts playback again; the hook only calls it when the Learner has not. */
  play: () => void;
};

const reasonOf = (n: VoiceNotice) =>
  n.kind === "error"
    ? n.cls
    : n.kind === "empty"
      ? "empty"
      : n.kind === "too_short" || n.kind === "silent"
        ? "too_short"
        : "denied";

/** The mounted Voice note control, so a hotkey layer can drive it without owning any UI. */
let active: RefObject<VoiceBubbleControls | null> | null = null;

/** Press the Voice note (hotkey: hold `N`). False when no Voice note is on screen. */
export function startVoiceQuickNote(): boolean {
  const controls = active?.current;
  if (!controls) return false;
  controls.press();
  return true;
}

/** Release the Voice note: transcribe and save. */
export function stopVoiceQuickNote(): boolean {
  const controls = active?.current;
  if (!controls) return false;
  controls.release();
  return true;
}

/**
 * Headless Voice quick note: pause on press, save the transcript at once as a raw note anchored
 * to the moment of the press, offer Undo, resume only what it paused. Wire the returned props
 * to `VoiceBubble` and pass `controls` for outside actions.
 */
export function useVoiceQuickNote({
  getPlayer,
  surface,
  controls,
}: {
  getPlayer: () => QuickNotePlayer;
  surface: VoiceQuickNoteSurface;
  controls: RefObject<VoiceBubbleControls | null>;
}) {
  const { t } = useUiPreferences();
  const latest = useRef({ getPlayer, surface, t });
  useEffect(() => {
    latest.current = { getPlayer, surface, t };
  });
  // The moment of the press: the note belongs there, not where the Learner is at release.
  const pressed = useRef<{
    at: number;
    anchor: NoteAnchor;
    ctx: Pick<
      QuickNotePlayer,
      "courseId" | "lessonId" | "slideIndex" | "slideTitle"
    >;
  } | null>(null);
  const pausedByUs = useRef(false);

  useEffect(() => {
    active = controls;
    return () => {
      if (active === controls) active = null;
    };
  }, [controls]);

  const onRecording = useCallback((recording: boolean) => {
    const p = latest.current.getPlayer();
    if (recording) {
      pressed.current = {
        at: Date.now(),
        anchor: {
          playbackMs: Math.round(p.timeMs),
          rect: p.region,
          input: "voice",
        },
        ctx: {
          courseId: p.courseId,
          lessonId: p.lessonId,
          slideIndex: p.slideIndex,
          slideTitle: p.slideTitle,
        },
      };
      if (p.playing) {
        pausedByUs.current = true;
        p.pause();
      }
      trackVoiceQuickNote({
        event: "voice_quick_note_started",
        surface: latest.current.surface,
        has_region: !!p.region,
      });
      return;
    }
    // Only resume what we stopped, and only if the Learner has not pressed Play meanwhile.
    if (pausedByUs.current) {
      pausedByUs.current = false;
      if (!p.playing) p.play();
    }
  }, []);

  const onTranscript = useCallback(async ({ text, language }: Transcript) => {
    const start = pressed.current;
    pressed.current = null;
    const body = text.trim().slice(0, 2000);
    const { surface: s, t: tr } = latest.current;
    if (!start || !body) return;
    const duration = Date.now() - start.at;
    const online = typeof navigator === "undefined" || navigator.onLine;
    const saved = await saveNote({
      context: {
        source: "slide",
        courseId: start.ctx.courseId,
        lessonId: start.ctx.lessonId,
        slideId: start.ctx.slideIndex,
        slideTitle: start.ctx.slideTitle,
        anchor: start.anchor,
      },
      body,
      voice: true,
      online,
    });
    trackVoiceQuickNote({
      event: "voice_quick_note_saved",
      surface: s,
      duration_ms: duration,
      has_region: !!start.anchor.rect,
      language,
    });
    showToast({
      id: "note-saved",
      message: tr("imm.saved"),
      action: {
        label: tr("common.undo"),
        onAction: () => {
          void undoSavedNote(saved);
          trackVoiceQuickNote({ event: "voice_quick_note_undone", surface: s });
        },
      },
    });
  }, []);

  const onNotice = useCallback((n: VoiceNotice) => {
    if (n.kind === "no_microphone") return;
    trackVoiceQuickNote({
      event: "voice_quick_note_failed",
      surface: latest.current.surface,
      reason: reasonOf(n),
    });
  }, []);

  const onCancelled = useCallback(() => {
    pressed.current = null;
    trackVoiceQuickNote({
      event: "voice_quick_note_cancelled",
      surface: latest.current.surface,
    });
  }, []);

  return { onRecording, onTranscript, onNotice, onCancelled };
}
