import { useEffect, useRef, useState } from "react";
import { ResponsivePanel } from "../shell/AdaptiveLayouts";
import { trackGuidedUi, trackVoice } from "../../lib/analytics";
import {
  anchorToWire,
  noteQueue,
  type NoteAnchor,
  type NoteSource,
  type QueuedNote,
} from "./note-queue";
import { session } from "../accounts/session";
import { AppLink } from "../../lib/navigation";
import { VoiceButton } from "../voice/VoiceButton";
import { useUiPreferences } from "../design/localization/Preferences";

export type QuickNoteContext = {
  source: NoteSource;
  courseId?: string;
  lessonId?: string;
  slideId?: number;
  slideTitle?: string;
  /** Pins the note to a moment (and region) of the Lesson. A synced anchored note is not reopened. */
  anchor?: NoteAnchor;
};

export type QuickNotePanelProps = {
  open: boolean;
  onClose: () => void;
  context: QuickNoteContext;
  onSync?: (note: {
    actionKey: string;
    courseId?: string;
    lessonId?: string;
    slideId?: number;
    slideTitle?: string;
    source: NoteSource;
    anchor?: NoteAnchor;
    rawText: string;
  }) => Promise<{ id: string; status: string }>;
  isOffline?: boolean;
  /** "bubble": a round mic icon; the dictated-note confirmation is a toast with Undo. */
  voiceVariant?: "default" | "bubble";
};

const ENRICH_LABEL = "Enrich with AI · 2 credits (daily free)"; // @noncommercial-number

const quickNoteDrafts = new Map<string, string>();

function getDraftKey(ctx: QuickNoteContext): string {
  return `${ctx.source}:${ctx.courseId ?? ""}:${ctx.lessonId ?? ""}:${ctx.slideId ?? ""}`;
}

export function clearQuickNoteDrafts(): void {
  quickNoteDrafts.clear();
}

export function QuickNotePanel({
  open,
  onClose,
  context,
  onSync,
  isOffline = false,
  voiceVariant = "default",
}: QuickNotePanelProps) {
  const { t } = useUiPreferences();
  const draftKey = getDraftKey(context);
  const [activeNote, setActiveNote] = useState<QueuedNote | null>(() => {
    // An anchored note is a new moment each time: only a still-unsynced one is reopened.
    const queued = noteQueue
      .getQueue()
      .filter(
        (item) => !context.anchor || (item.anchor && item.status !== "synced"),
      );
    return (
      queued.find((item) => {
        if (item.source !== context.source) return false;
        if (context.lessonId && item.lessonId !== context.lessonId)
          return false;
        if (context.slideId !== undefined && item.slideId !== context.slideId)
          return false;
        return true;
      }) ?? null
    );
  });
  const [noteText, setNoteTextState] = useState(
    () => activeNote?.rawText ?? quickNoteDrafts.get(draftKey) ?? "",
  );
  const setNoteText = (val: string) => {
    setNoteTextState(val);
    if (!activeNote) {
      if (val) {
        quickNoteDrafts.set(draftKey, val);
      } else {
        quickNoteDrafts.delete(draftKey);
      }
    }
  };
  const [enrichWithAi, setEnrichWithAi] = useState(true);
  const [status, setStatus] = useState<"draft" | "offline_saved" | "synced">(
    () =>
      activeNote
        ? activeNote.status === "synced"
          ? "synced"
          : "offline_saved"
        : "draft",
  );
  const [isSyncing, setIsSyncing] = useState(false);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const reportedOpen = useRef(false);

  useEffect(() => {
    if (open) {
      if (!reportedOpen.current) {
        reportedOpen.current = true;
        trackGuidedUi({ event: "ui_sheet_opened", sheetId: "quick_note" });
      }
      if (status === "draft" && !activeNote) {
        setTimeout(() => textareaRef.current?.focus(), 50);
      }
    } else {
      reportedOpen.current = false;
    }
  }, [open, status, activeNote]);

  // A dictated note is saved at once; Undo removes it again (device queue and, when synced, the server).
  const [dictated, setDictated] = useState<{
    actionKey: string;
    id?: string;
  } | null>(null);

  const handleSave = async (dictation?: string) => {
    const body = dictation ?? noteText;
    const trimmed = body.trim();
    if (!trimmed) return;
    quickNoteDrafts.delete(draftKey);
    const anchor: NoteAnchor | undefined =
      dictation !== undefined && context.anchor
        ? { ...context.anchor, input: "voice" }
        : context.anchor;

    // Save to local device queue with stable action key
    const queued = noteQueue.enqueue({
      courseId: context.courseId,
      lessonId: context.lessonId,
      slideId: context.slideId,
      slideTitle: context.slideTitle,
      source: context.source,
      anchor,
      rawText: body,
      status: "device_saved",
    });
    setActiveNote(queued);
    if (dictation !== undefined) setDictated({ actionKey: queued.actionKey });

    // If marked offline or browser navigator is offline, keep as offline_saved
    const online =
      !isOffline && (typeof navigator === "undefined" || navigator.onLine);
    if (!online) {
      setStatus("offline_saved");
      return;
    }

    // Attempt online sync
    setIsSyncing(true);
    try {
      if (onSync) {
        const res = await onSync({
          actionKey: queued.actionKey,
          courseId: context.courseId,
          lessonId: context.lessonId,
          slideId: context.slideId,
          slideTitle: context.slideTitle,
          source: context.source,
          anchor,
          rawText: body,
        });
        noteQueue.markSynced(queued.actionKey, res.id);
        if (dictation !== undefined)
          setDictated({ actionKey: queued.actionKey, id: res.id });
        setStatus("synced");
      } else {
        const res = await session.request<{ id: string; status: string }>(
          "/v1/notes",
          "POST",
          {
            action_key: queued.actionKey,
            course_id: context.courseId,
            lesson_id: context.lessonId,
            slide_id: context.slideId,
            slide_title: context.slideTitle,
            source: context.source,
            anchor: anchorToWire(anchor),
            raw_text: body,
          },
        );
        noteQueue.markSynced(queued.actionKey, res.id);
        if (dictation !== undefined)
          setDictated({ actionKey: queued.actionKey, id: res.id });
        setStatus("synced");
      }
    } catch {
      setStatus("offline_saved");
    } finally {
      setIsSyncing(false);
    }
  };

  const undoDictation = async () => {
    if (!dictated) return;
    const { actionKey, id } = dictated;
    noteQueue.remove(actionKey);
    setDictated(null);
    setActiveNote(null);
    setNoteTextState("");
    setStatus("draft");
    trackVoice({
      event: "voice_transcript_discarded",
      purpose: "note",
      entry_point: "quick_note",
      edited: false,
    });
    if (id) {
      try {
        await session.request(`/v1/notes/${id}`, "DELETE");
      } catch {
        /* The note stays in Notes and can be deleted there. */
      }
    }
  };

  const undoRef = useRef(undoDictation);
  useEffect(() => {
    undoRef.current = undoDictation;
  });
  const bubble = voiceVariant === "bubble";
  const dictatedKey = dictated?.actionKey;
  useEffect(() => {
    if (!bubble || !dictatedKey) return;
    // Loaded on demand: toasts belong to the immersive presentation, not the first screen.
    void import("../../components/toast").then(({ showToast }) =>
      showToast({
        id: "note-saved",
        message: t("imm.saved"),
        action: {
          label: t("common.undo"),
          onAction: () => void undoRef.current(),
        },
      }),
    );
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [dictatedKey, bubble]);

  const handleManualSync = async () => {
    if (!activeNote) return;
    setIsSyncing(true);
    try {
      if (onSync) {
        const res = await onSync({
          actionKey: activeNote.actionKey,
          courseId: activeNote.courseId,
          lessonId: activeNote.lessonId,
          slideId: activeNote.slideId,
          slideTitle: activeNote.slideTitle,
          source: activeNote.source,
          anchor: activeNote.anchor,
          rawText: activeNote.rawText,
        });
        noteQueue.markSynced(activeNote.actionKey, res.id);
        setStatus("synced");
      } else {
        const res = await session.request<{ id: string; status: string }>(
          "/v1/notes",
          "POST",
          {
            action_key: activeNote.actionKey,
            course_id: activeNote.courseId,
            lesson_id: activeNote.lessonId,
            slide_id: activeNote.slideId,
            slide_title: activeNote.slideTitle,
            source: activeNote.source,
            anchor: anchorToWire(activeNote.anchor),
            raw_text: activeNote.rawText,
          },
        );
        noteQueue.markSynced(activeNote.actionKey, res.id);
        setStatus("synced");
      }
    } catch {
      setStatus("offline_saved");
    } finally {
      setIsSyncing(false);
    }
  };

  return (
    <ResponsivePanel title="Quick note" open={open} onClose={onClose}>
      <div
        className="quick-note-content"
        style={{
          display: "flex",
          flexDirection: "column",
          gap: "12px",
          padding: "16px",
        }}
      >
        {context.slideTitle && (
          <span
            className="chip context-chip"
            style={{
              display: "inline-flex",
              alignSelf: "flex-start",
              padding: "4px 10px",
              borderRadius: "12px",
              background: "#F1F1F1",
              fontSize: "13px",
              fontWeight: 800,
              color: "#444",
            }}
          >
            {context.slideTitle}
          </span>
        )}

        {context.lessonId && (status === "draft" || dictated) && (
          <VoiceButton
            purpose="note"
            entryPoint="quick_note"
            lessonId={context.lessonId}
            disabled={isSyncing}
            variant={voiceVariant}
            onTranscript={({ text }) => {
              trackVoice({
                event: "voice_transcript_used",
                purpose: "note",
                entry_point: "quick_note",
                edited: false,
              });
              void handleSave(text.slice(0, 2000));
            }}
          />
        )}
        {dictated && !bubble && (
          <p role="status" data-testid="voice-note-saved" style={{ margin: 0 }}>
            Saved your dictated note.{" "}
            <button
              type="button"
              data-testid="voice-note-undo"
              disabled={isSyncing}
              onClick={() => void undoDictation()}
              style={{ fontWeight: 800 }}
            >
              Undo
            </button>
          </p>
        )}

        {status === "draft" && (
          <>
            <label
              style={{
                display: "flex",
                flexDirection: "column",
                gap: "6px",
                fontWeight: 700,
              }}
            >
              Your note
              <textarea
                ref={textareaRef}
                rows={5}
                maxLength={2000}
                value={noteText}
                onChange={(e) => setNoteText(e.target.value)}
                placeholder='e.g. Stars "move" because Earth spins west → east.'
                style={{
                  width: "100%",
                  padding: "10px",
                  borderRadius: "12px",
                  border: "2px solid #CFCFCF",
                  fontFamily: "inherit",
                  fontSize: "15px",
                  resize: "vertical",
                  boxSizing: "border-box",
                }}
              />
            </label>

            <span style={{ fontSize: "13px", color: "#666", fontWeight: 600 }}>
              {noteText.length} / 2,000 · Saved text stays yours. AI adds a
              separate summary.
            </span>

            <label
              style={{
                display: "flex",
                alignItems: "center",
                gap: "10px",
                fontWeight: 800,
                fontSize: "14px",
                cursor: "pointer",
              }}
            >
              <input
                type="checkbox"
                checked={enrichWithAi}
                onChange={(e) => setEnrichWithAi(e.target.checked)}
                style={{
                  width: "20px",
                  height: "20px",
                  accentColor: "#1F7A0D",
                }}
              />
              {ENRICH_LABEL}
            </label>

            <button
              type="button"
              className="duo-button primary"
              onClick={() => void handleSave()}
              disabled={!noteText.trim()}
              style={{
                display: "flex",
                alignItems: "center",
                justifyContent: "center",
                minHeight: "48px",
                padding: "0 16px",
                borderRadius: "14px",
                background: noteText.trim() ? "#1F7A0D" : "#E0E0E0",
                color: noteText.trim() ? "#fff" : "#999",
                fontWeight: 900,
                fontSize: "15px",
                border: "none",
                cursor: noteText.trim() ? "pointer" : "not-allowed",
              }}
            >
              Save quick note
            </button>
          </>
        )}

        {status === "offline_saved" && (
          <div
            style={{ display: "flex", flexDirection: "column", gap: "12px" }}
          >
            <div
              className="ban warn"
              role="status"
              style={{
                borderRadius: "16px",
                padding: "12px 14px",
                background: "#FFF1D1",
                color: "#704400",
                display: "flex",
                flexDirection: "column",
                gap: "4px",
              }}
            >
              <b style={{ fontSize: "15px", fontWeight: 900 }}>
                Offline variant · saved on this device
              </b>
              <p style={{ margin: 0, fontSize: "13px" }}>
                It will sync once you're online. Not backed up yet.
              </p>
            </div>

            <div style={{ display: "flex", gap: "10px" }}>
              <button
                type="button"
                className="duo-button secondary"
                onClick={handleManualSync}
                disabled={isSyncing}
                style={{
                  flex: 1,
                  minHeight: "44px",
                  borderRadius: "14px",
                  border: "2px solid #D3D3D3",
                  background: "#fff",
                  color: "#1667AB",
                  fontWeight: 900,
                  cursor: "pointer",
                }}
              >
                {isSyncing ? "Syncing…" : "Sync now"}
              </button>
              <button
                type="button"
                className="duo-button"
                onClick={onClose}
                style={{
                  flex: 1,
                  minHeight: "44px",
                  borderRadius: "14px",
                  border: "none",
                  background: "#1F7A0D",
                  color: "#fff",
                  fontWeight: 900,
                  cursor: "pointer",
                }}
              >
                Back to lesson
              </button>
            </div>
          </div>
        )}

        {status === "synced" && (
          <div
            style={{ display: "flex", flexDirection: "column", gap: "12px" }}
          >
            <div
              className="ban ok"
              role="status"
              style={{
                borderRadius: "16px",
                padding: "12px 14px",
                background: "#E3F8D3",
                color: "#1E6E0C",
                display: "flex",
                flexDirection: "column",
                gap: "4px",
              }}
            >
              <b style={{ fontSize: "15px", fontWeight: 900 }}>
                Saved. Enrichment is pending.
              </b>
              <p style={{ margin: 0, fontSize: "13px" }}>
                Synced to your account. The AI summary and tags appear in Quick
                notes.
              </p>
            </div>

            <div
              className="ban credit"
              style={{
                borderRadius: "16px",
                padding: "12px 14px",
                background: "#F0E7FF",
                color: "#5B2BB5",
                display: "flex",
                flexDirection: "column",
                gap: "4px",
              }}
            >
              <b style={{ fontSize: "15px", fontWeight: 900 }}>
                Zero-credit variant
              </b>
              <p style={{ margin: 0, fontSize: "13px" }}>
                Your note is saved. Add credits to enrich it, or wait for your
                daily reset.
              </p>
            </div>

            <div style={{ display: "flex", gap: "10px" }}>
              <AppLink
                href="/notes"
                className="duo-button secondary"
                style={{
                  flex: 1,
                  display: "flex",
                  alignItems: "center",
                  justifyContent: "center",
                  minHeight: "44px",
                  borderRadius: "14px",
                  border: "2px solid #D3D3D3",
                  background: "#fff",
                  color: "#1667AB",
                  fontWeight: 900,
                  textDecoration: "none",
                }}
              >
                Open notes
              </AppLink>
              <button
                type="button"
                className="duo-button primary"
                onClick={onClose}
                style={{
                  flex: 1,
                  minHeight: "44px",
                  borderRadius: "14px",
                  border: "none",
                  background: "#1F7A0D",
                  color: "#fff",
                  fontWeight: 900,
                  cursor: "pointer",
                }}
              >
                Back to lesson
              </button>
            </div>
          </div>
        )}
      </div>
    </ResponsivePanel>
  );
}
