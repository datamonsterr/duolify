import { useEffect, useRef, useState } from "react";
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
 * The opaque answer panel docked to the right of the immersive stage: the Learner's question,
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
      await session.request("/v1/notes", "POST", {
        action_key: `region-answer:${id}`,
        course_id: courseId,
        lesson_id: lessonId,
        slide_id: slideIndex,
        slide_title: slideTitle,
        source: "slide",
        raw_text: text.slice(0, 2000),
        anchor: anchorToWire(anchor),
      });
      setNoteState((s) => ({ ...s, [id]: "saved" }));
    } catch {
      setNoteState((s) => ({ ...s, [id]: "error" }));
    }
  }

  if (collapsed)
    return (
      <button
        type="button"
        className="imm-btn imm-answer-handle"
        data-testid="answer-expand"
        aria-label="Show the answer panel"
        onClick={() => onCollapse(false)}
      >
        Answer
      </button>
    );

  const notice = tutor.notice;
  const refusal = !tutor.online
    ? "Asking needs a connection. Your question is kept."
    : notice?.kind === "credits"
      ? "Not enough Credits for this question."
      : notice?.kind === "failed"
        ? "The tutor could not answer. Nothing was charged. Try again."
        : notice?.kind === "stale"
          ? "The price changed. Send again."
          : notice?.kind === "load"
            ? "The conversation could not be loaded."
            : tutor.block
              ? "This question needs more context than a quick answer allows."
              : "";

  return (
    <aside
      className="imm-answer"
      data-testid="answer-panel"
      aria-label="Answer about this area"
      onKeyDown={(e) => {
        if (e.key === "Escape") {
          e.preventDefault();
          e.stopPropagation();
          close();
        }
      }}
    >
      <header className="imm-answer-head">
        <h2>{region ? "Ask about this area" : "Ask about this Slide"}</h2>
        <button
          type="button"
          className="imm-btn"
          data-testid="answer-collapse"
          aria-label="Collapse the answer panel"
          onClick={() => onCollapse(true)}
        >
          Hide
        </button>
        <button
          type="button"
          className="imm-btn"
          data-testid="answer-close"
          aria-label="Close the answer panel"
          onClick={close}
        >
          Close
        </button>
      </header>
      {earlier && (
        <button
          type="button"
          className="imm-btn"
          data-testid="answer-restore"
          onClick={() => onRestore(earlier.rect)}
        >
          Outline the area I asked about before
        </button>
      )}
      <div
        ref={log}
        className="imm-answer-log"
        role="log"
        aria-label="Conversation"
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
                  className="imm-btn"
                  data-testid="answer-save-note"
                  disabled={
                    noteState[m.id] === "saving" || noteState[m.id] === "saved"
                  }
                  onClick={() => void saveAsNote(m.id, m.content)}
                >
                  {noteState[m.id] === "saved"
                    ? "Saved as Quick note"
                    : noteState[m.id] === "error"
                      ? "Could not save. Try again"
                      : "Save as Quick note"}
                </button>
              )}
          </div>
        ))}
      </div>
      <p className="imm-sr" role="status" data-testid="answer-status">
        {streaming ? "Answering" : lastReply ? "Answer ready" : ""}
      </p>
      {refusal && (
        <p className="imm-answer-note" role="alert" data-testid="answer-notice">
          {refusal}
        </p>
      )}
      <form
        className="imm-answer-compose"
        onSubmit={(e) => {
          e.preventDefault();
          sendNow();
        }}
      >
        <label>
          <span className="imm-sr">Your question</span>
          <textarea
            ref={field}
            rows={2}
            value={tutor.draft}
            maxLength={2000}
            placeholder={shown.length ? "Ask a follow-up" : "Ask about this"}
            disabled={busy}
            onChange={(e) => {
              if (tutor.draft === "") setAsked("text");
              tutor.setDraft(e.target.value);
            }}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.shiftKey) {
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
          disabled={busy || !tutor.online}
          onTranscript={({ text, language }) => {
            setAsked("voice");
            tutor.setDraft(text);
            dictation.begin(text, language);
          }}
        />
        {streaming ? (
          <button
            type="button"
            className="imm-btn"
            data-testid="answer-stop"
            onClick={() => void tutor.stop()}
          >
            Stop
          </button>
        ) : (
          <button
            type="submit"
            className="imm-btn imm-btn-big"
            data-testid="answer-send"
            disabled={
              !tutor.draft.trim() || busy || !tutor.online || !!tutor.block
            }
          >
            {busy ? "Sending…" : "Send"}
          </button>
        )}
      </form>
    </aside>
  );
}
