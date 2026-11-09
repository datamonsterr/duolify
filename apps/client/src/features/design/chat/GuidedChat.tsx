import {
  useEffect,
  useId,
  useRef,
  useState,
  type MouseEventHandler,
} from "react";
import { trackGuidedUi } from "../../../lib/analytics";
import { Button } from "../Button";
import { Badge } from "../Badge";
import { Feedback } from "./Feedback";
import "./guided-chat.css";

export type ChatMessage = {
  id: string;
  author: "assistant" | "learner";
  authorLabel: string;
  text: string;
  status: "streaming" | "complete";
};
export type ChatQuestion = {
  id: string;
  replies: { id: string; label: string }[];
  multiple?: boolean;
};
export type ChatAnswer = { text: string; replyIds: string[]; skipped: boolean };
export type GuidedChatProps = {
  messages: ChatMessage[];
  question: ChatQuestion;
  draft: string;
  onDraftChange: (value: string) => void;
  onAnswer: (answer: ChatAnswer) => void;
  formHref: string;
  onFormFallback?: MouseEventHandler<HTMLAnchorElement>;
  step?: { current: number; total: number };
  state?: "ready" | "loading" | "offline" | "error";
  onRetry?: () => void;
};

export function GuidedChat({
  messages,
  question,
  draft,
  onDraftChange,
  onAnswer,
  formHref,
  onFormFallback,
  step,
  state = "ready",
  onRetry,
}: GuidedChatProps) {
  const inputId = useId();
  const composer = useRef<HTMLTextAreaElement>(null);
  const log = useRef<HTMLDivElement>(null);
  const announced = useRef(new Set<string>());
  const [selection, setSelection] = useState<{
    questionId: string;
    ids: string[];
  }>({ questionId: question.id, ids: [] });
  const selected = selection.questionId === question.id ? selection.ids : [];
  const pending = state === "loading";
  useEffect(() => {
    for (const message of messages) {
      if (
        message.author !== "assistant" ||
        message.status !== "complete" ||
        announced.current.has(message.id)
      )
        continue;
      announced.current.add(message.id);
      const line = document.createElement("p");
      line.textContent = `${message.authorLabel}: ${message.text}`;
      log.current?.append(line);
    }
  }, [messages]);

  function answer(skipped = false) {
    if (pending || (!skipped && !draft.trim() && !selected.length)) return;
    trackGuidedUi({
      event: "ui_control_activated",
      surface: "guided_chat",
      controlId: skipped ? "skip" : "send",
    });
    onAnswer({
      text: skipped ? "" : draft,
      replyIds: skipped ? [] : selected,
      skipped,
    });
    setSelection({ questionId: question.id, ids: [] });
    composer.current?.focus();
  }
  return (
    <section className="guided-chat" aria-label="Guided chat">
      <div className="guided-chat-header">
        {step && (
          <>
            <Badge role="info">
              Step {step.current} of {step.total}
            </Badge>
            <progress
              aria-label="Chat steps"
              value={step.current}
              max={step.total}
            />
          </>
        )}
        <a
          className="guided-link"
          href={formHref}
          onClick={(event) => {
            trackGuidedUi({
              event: "ui_control_activated",
              surface: "guided_chat",
              controlId: "form_fallback",
            });
            onFormFallback?.(event);
          }}
        >
          Prefer a form?
        </a>
      </div>
      <div
        className="guided-transcript"
        role="region"
        aria-label="Chat transcript"
        aria-live="off"
      >
        {messages.length === 0 && (
          <p>No messages yet. You can start with an answer.</p>
        )}
        {messages.map((message) => (
          <div className={`guided-message ${message.author}`} key={message.id}>
            <strong>{message.authorLabel}</strong>
            <div className="guided-bubble">
              {message.text}
              {message.status === "streaming" && (
                <span className="field-hint"> (composing)</span>
              )}
            </div>
          </div>
        ))}
      </div>
      <div
        ref={log}
        className="guided-sr-only"
        role="log"
        aria-label="New assistant messages"
        aria-live="polite"
        aria-relevant="additions"
      />
      {pending && (
        <Feedback title="Duolify is typing…">
          Sending is unavailable while a reply is being prepared.
        </Feedback>
      )}
      {state === "offline" && (
        <Feedback
          tone="warning"
          title="Offline. Your draft stays here; use the form or retry when connected."
          action="Retry chat"
          onAction={onRetry}
        />
      )}
      {state === "error" && (
        <Feedback
          tone="danger"
          title="Chat is unavailable"
          blocking
          action="Retry chat"
          onAction={() => {
            trackGuidedUi({
              event: "ui_control_activated",
              surface: "guided_chat",
              controlId: "retry",
            });
            onRetry?.();
          }}
        >
          Your answers stay here. Use the form to keep going.
        </Feedback>
      )}
      <div
        role="group"
        aria-label={
          question.multiple ? "Quick replies, choose any" : "Quick replies"
        }
        className="guided-actions"
      >
        {question.replies.map((reply) => (
          <Button
            key={reply.id}
            variant="quiet"
            aria-label={reply.label}
            disabled={pending}
            aria-pressed={selected.includes(reply.id)}
            onClick={() => {
              const ids = selected.includes(reply.id)
                ? selected.filter((id) => id !== reply.id)
                : question.multiple
                  ? [...selected, reply.id]
                  : [reply.id];
              setSelection({ questionId: question.id, ids });
              trackGuidedUi({
                event: "ui_control_activated",
                surface: "guided_chat",
                controlId: "quick_reply",
              });
              composer.current?.focus();
            }}
          >
            {reply.label}
          </Button>
        ))}
        <Button variant="quiet" onClick={() => answer(true)} disabled={pending}>
          Skip
        </Button>
      </div>
      <form
        className="guided-composer"
        onSubmit={(event) => {
          event.preventDefault();
          answer();
        }}
      >
        <label htmlFor={inputId}>Your answer</label>
        <textarea
          id={inputId}
          ref={composer}
          rows={2}
          value={draft}
          onChange={(event) => onDraftChange(event.target.value)}
        />
        <Button
          type="submit"
          disabled={pending || (!draft.trim() && !selected.length)}
          aria-busy={pending || undefined}
        >
          Send answer
        </Button>
      </form>
    </section>
  );
}
