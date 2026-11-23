import { useEffect, useRef, useState } from "react";
import { showToast, dismissToast } from "../../../components/toast";
import { emitMascotTalk } from "../../../components/mascot/events";
import { Icon } from "../../design/Icon";
import { useUiPreferences } from "../../design/localization/Preferences";
import { session } from "../../accounts/session";
import { tutorService, type RegionAsk } from "../../chat/tutor";
import { useTutor } from "../../chat/useTutor";
import type { NoteAnchor } from "../../notes/note-queue";
import { anchorToWire } from "../../notes/note-queue";
import { VoiceButton } from "../../voice/VoiceButton";
import { DictationCountdown } from "../../voice/DictationCountdown";
import { useDictationWindow } from "../../voice/useDictationWindow";
import type { RegionRect } from "./gestures";

export type PanelOutcome = {
  outcome: "completed" | "stopped" | "failed";
  savedNote: boolean;
  followUps: number;
};

type Props = {
  courseId: string;
  lessonId: string;
  slideIndex: number;
  slideTitle: string;
  /** The region the question is about; absent: the whole Slide. */
  region?: RegionRect;
  atMs: number;
  collapsed: boolean;
  onCollapse: (collapsed: boolean) => void;
  /** Re-ask about, and outline, an area asked about earlier on this Slide. */
  onRestore: (rect: RegionRect) => void;
  onClose: (result: PanelOutcome) => void;
};

/**
 * The translucent answer panel docked to the right of the immersive stage: the Learner's question,
 * the streamed reply (Stop while it streams), follow-ups, save as an anchored Quick note,
 * collapse and close. It shares the lesson tutor thread with the tutor sheet.
 */
export default function AnswerPanel({
  courseId,
  lessonId,
  slideIndex,
  slideTitle,
  region,
  atMs,
  collapsed,
  onCollapse,
  onRestore,
  onClose,
}: Props) {
  // "voice" from the moment a dictated question lands in the composer until the Learner types fresh.
  const [asked, setAsked] = useState<"text" | "voice">("text");
  const ask: RegionAsk | undefined = region
    ? { rect: region, at_ms: Math.round(atMs), input: asked }
    : undefined;
  const { t } = useUiPreferences();
  const tutor = useTutor(
    courseId,
    tutorService,
    true,
    { lesson_id: lessonId, slide_index: slideIndex },
    ask,
  );
  const dictation = useDictationWindow({
    entryPoint: "answer_panel",
    draft: tutor.draft,
    send: () => sendNow(),
  });
  const sendNow = () => {
    dictation.noteSent();
    tutor.send();
  };
  // Only what was asked in this panel is shown; earlier turns stay in the shared thread.
  const [baseline, setBaseline] = useState<Set<string> | null>(null);
  if (baseline === null && !tutor.loading)
    setBaseline(new Set(tutor.messages.map((m) => m.id)));
  const shown = tutor.messages.filter((m) => baseline && !baseline.has(m.id));
  const field = useRef<HTMLTextAreaElement>(null);
  const log = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!collapsed) field.current?.focus({ preventScroll: true });
  }, [collapsed]);
  const lastContent = shown[shown.length - 1]?.content;
  useEffect(() => {
    log.current?.scrollTo?.({ top: log.current.scrollHeight });
  }, [shown.length, lastContent]);

  const [noteState, setNoteState] = useState<
    Record<string, "saving" | "saved" | "error">
  >({});
  const savedAny = Object.values(noteState).includes("saved");
  const busy = tutor.phase !== "idle";
  const streaming = tutor.phase === "streaming";
  const questions = shown.filter((m) => m.role === "user").length;
  const lastReply = [...shown].reverse().find((m) => m.role === "assistant");

  // The thread remembers the area of each earlier question on this Slide.
  const earlier = region
    ? undefined
    : [...tutor.messages]
        .reverse()
        .find(
          (m) =>
            m.anchor &&
            m.anchor.slide_index === slideIndex &&
            (!m.anchor.lesson_id || m.anchor.lesson_id === lessonId),
        )?.anchor;

  const close = () => {
    const failed =
      tutor.notice?.kind === "failed" || lastReply?.status === "interrupted";
    onClose({
      outcome: failed
        ? "failed"
        : lastReply?.status === "stopped" || tutor.notice?.kind === "stopped"
          ? "stopped"
          : "completed",
      savedNote: savedAny,
      followUps: Math.max(0, questions - 1),
    });
  };

  async function saveAsNote(id: string, text: string) {
    setNoteState((s) => ({ ...s, [id]: "saving" }));
    const anchor: NoteAnchor = {
      playbackMs: Math.round(atMs),
      rect: region,
      input: "typed",
    };
    try {
      const saved = await session.request<{ id?: string }>(
        "/v1/notes",
        "POST",
        {
          action_key: `region-answer:${id}`,
          course_id: courseId,
          lesson_id: lessonId,
          slide_id: slideIndex,
          slide_title: slideTitle,
          source: "slide",
          raw_text: text.slice(0, 2000),
          anchor: anchorToWire(anchor),
        },
      );
      setNoteState((s) => ({ ...s, [id]: "saved" }));
      showToast({
        id: "note-saved",
        message: t("imm.saved"),
        action: saved?.id
          ? {
              label: t("common.undo"),
              onAction: () => {
                void session
                  .request(`/v1/notes/${saved.id}`, "DELETE")
                  .catch(() => {});
                setNoteState((s) => {
                  const rest = { ...s };
                  delete rest[id];
                  return rest;
                });
              },
            }
          : undefined,
      });
    } catch {
      setNoteState((s) => ({ ...s, [id]: "error" }));
      showToast({
        id: "note-saved",
        message: t("imm.failed"),
        tone: "error",
      });
    }
  }

  const notice = tutor.notice;
  const refusal = !tutor.online
    ? t("imm.offline")
    : notice?.kind === "credits"
      ? t("imm.credits")
      : notice?.kind === "failed" ||
          notice?.kind === "stale" ||
          notice?.kind === "load"
        ? t("imm.failed")
        : tutor.block
          ? t("imm.block")
          : "";
  useEffect(() => {
    if (refusal)
      showToast({ id: "answer-notice", message: refusal, tone: "error" });
    else dismissToast("answer-notice");
  }, [refusal]);
  // The mascot companion talks while a reply streams.
  useEffect(() => {
    emitMascotTalk(streaming);
    return () => void emitMascotTalk(false);
  }, [streaming]);

  if (collapsed)
    return (
      <button
        type="button"
        className="imm-btn imm-icon-btn imm-answer-handle"
        data-testid="answer-expand"
        aria-label={t("imm.panel_label")}
        title={t("imm.panel_label")}
        onClick={() => onCollapse(false)}
      >
        <Icon name="ask" />
      </button>
    );

  const cannotSend =
    !tutor.draft.trim() || busy || !tutor.online || !!tutor.block;
  const sendReason =
    cannotSend && tutor.draft.trim() ? refusal || t("imm.answering") : "";

  return (
    <aside
      className="imm-answer"
      data-testid="answer-panel"
      aria-label={t("imm.panel_label")}
      onKeyDown={(e) => {
        if (e.key === "Escape") {
          e.preventDefault();
          e.stopPropagation();
          close();
        }
      }}
    >
      <header className="imm-answer-head">
        {earlier && (
          <button
            type="button"
            className="imm-btn imm-icon-btn"
            data-testid="answer-restore"
            aria-label={t("imm.select_area")}
            title={t("imm.select_area")}
            onClick={() => onRestore(earlier.rect)}
          >
            <Icon name="select" />
          </button>
        )}
        <span className="imm-grow" />
        <button
          type="button"
          className="imm-btn imm-icon-btn"
          data-testid="answer-collapse"
          aria-label={t("imm.hide")}
          title={t("imm.hide")}
          onClick={() => onCollapse(true)}
        >
          <Icon name="chevron" />
        </button>
        <button
          type="button"
          className="imm-btn imm-icon-btn"
          data-testid="answer-close"
          aria-label={t("common.close")}
          title={t("common.close")}
          onClick={close}
        >
          <Icon name="close" />
        </button>
      </header>
      <div
        ref={log}
        className="imm-answer-log"
        role="log"
        aria-label={t("imm.panel_label")}
        aria-live="off"
      >
        {shown.map((m) => (
          <div key={m.id} className={`imm-answer-msg imm-answer-${m.role}`}>
            <p>{m.content || (m.status === "streaming" ? "…" : "")}</p>
            {m.role === "assistant" &&
              m.status !== "streaming" &&
              m.content && (
                <button
                  type="button"
                  className="imm-btn imm-icon-btn"
                  data-testid="answer-save-note"
                  aria-label={
                    noteState[m.id] === "saved" ? t("imm.saved") : t("imm.note")
                  }
                  title={
                    noteState[m.id] === "saved" ? t("imm.saved") : t("imm.note")
                  }
                  aria-pressed={noteState[m.id] === "saved"}
                  disabled={
                    noteState[m.id] === "saving" || noteState[m.id] === "saved"
                  }
                  onClick={() => void saveAsNote(m.id, m.content)}
                >
                  <Icon name={noteState[m.id] === "saved" ? "check" : "note"} />
                </button>
              )}
          </div>
        ))}
      </div>
      <p className="imm-sr" role="status" data-testid="answer-status">
        {streaming ? t("imm.answering") : lastReply ? t("imm.ready") : ""}
      </p>
      <form
        className="imm-answer-compose"
        onSubmit={(e) => {
          e.preventDefault();
          sendNow();
        }}
      >
        <label>
          <span className="imm-sr">{t("imm.ask_ph")}</span>
          <textarea
            ref={field}
            rows={2}
            value={tutor.draft}
            maxLength={2000}
            placeholder={shown.length ? t("imm.ask_ph") : t("imm.ask_ph")}
            disabled={busy}
            onChange={(e) => {
              if (tutor.draft === "") setAsked("text");
              tutor.setDraft(e.target.value);
            }}
            onKeyDown={(e) => {
              if (
                e.key === "Enter" &&
                !e.shiftKey &&
                !e.nativeEvent.isComposing
              ) {
                e.preventDefault();
                sendNow();
              }
            }}
          />
        </label>
        <DictationCountdown
          counting={dictation.counting}
          onCancel={dictation.cancelAuto}
        />
        <VoiceButton
          purpose="tutor"
          entryPoint="answer_panel"
          lessonId={lessonId}
          variant="bubble"
          disabled={busy || !tutor.online}
          disabledReason={
            !tutor.online
              ? t("imm.offline")
              : busy
                ? t("imm.answering")
                : undefined
          }
          onTranscript={({ text, language }) => {
            setAsked("voice");
            tutor.setDraft(text);
            dictation.begin(text, language);
          }}
        />
        {streaming ? (
          <button
            type="button"
            className="imm-btn imm-icon-btn"
            data-testid="answer-stop"
            aria-label={t("imm.stop")}
            title={t("imm.stop")}
            onClick={() => void tutor.stop()}
          >
            <Icon name="stop" />
          </button>
        ) : (
          <button
            type="submit"
            className="imm-btn imm-icon-btn imm-btn-big"
            data-testid="answer-send"
            aria-label={t("tutor.send")}
            title={sendReason || t("tutor.send")}
            aria-describedby={sendReason ? "answer-send-reason" : undefined}
            disabled={cannotSend}
          >
            <Icon name="send" />
          </button>
        )}
        {sendReason && (
          <span id="answer-send-reason" hidden>
            {sendReason}
          </span>
        )}
      </form>
    </aside>
  );
}
