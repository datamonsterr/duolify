import { useEffect, useRef, useState, type FormEvent } from "react";
import { Button } from "../design/Button";
import { LearnerContextEditor } from "./LearnerContextEditor";
import {
  contextValues,
  profileApi,
  type ContextFields,
  type LearnerContext,
} from "./learner-context";
import { trackGuidedUi } from "../../lib/analytics";
import "./setup-chat.css";

export interface SetupChatTurnResponse {
  message: string;
  quick_replies?: { id: string; label: string }[];
  updated_profile?: Partial<ContextFields>;
  next_step: string;
  is_follow_up: boolean;
  is_complete: boolean;
}

export interface SetupChatProps {
  profile: LearnerContext;
  onCommit: (
    action: "save" | "skip",
    fields: Partial<ContextFields>,
  ) => Promise<LearnerContext>;
  chatClient?: (turn: {
    current_step: string;
    answer?: string;
    skipped?: boolean;
    is_follow_up?: boolean;
    draft?: Partial<ContextFields>;
  }) => Promise<SetupChatTurnResponse>;
  onExport?: () => Promise<void>;
}

const STEP_INDEX: Record<string, number> = {
  goal: 1,
  level: 2,
  daily_time: 3,
  reminders: 4,
  time_zone: 5,
  confirm: 5,
};

const STEP_NAMES: Record<string, string> = {
  goal: "Your goal",
  level: "What you know",
  daily_time: "Daily time",
  reminders: "Reminders",
  time_zone: "Time zone",
  confirm: "Review",
};

interface ChatMessage {
  id: string;
  author: "assistant" | "learner";
  authorLabel: string;
  text: string;
}

export function SetupChat({
  profile,
  onCommit,
  chatClient = profileApi.setupChat,
  onExport,
}: SetupChatProps) {
  const [draft, setDraft] = useState<ContextFields>(() =>
    contextValues(profile),
  );
  const [currentStep, setCurrentStep] = useState<string>("goal");
  const [isFollowUp, setIsFollowUp] = useState(false);
  const [isComplete, setIsComplete] = useState(false);
  const [composerText, setComposerText] = useState("");
  const [pending, setPending] = useState(false);
  const [fallback, setFallback] = useState(false);
  const [commitError, setCommitError] = useState("");
  const [completed, setCompleted] = useState(false);
  const [summaryOpen, setSummaryOpen] = useState(false);
  const [editingField, setEditingField] = useState<keyof ContextFields | null>(
    null,
  );
  const [editValue, setEditValue] = useState("");

  const [messages, setMessages] = useState<ChatMessage[]>([
    {
      id: "msg-0",
      author: "assistant",
      authorLabel: "Duolify",
      text: "What would you like to get better at?",
    },
  ]);

  const [quickReplies, setQuickReplies] = useState<
    { id: string; label: string }[]
  >([
    { id: "science", label: "Science" },
    { id: "language", label: "A language" },
    { id: "coding", label: "Coding" },
    { id: "history", label: "History" },
    { id: "skip", label: "Skip" },
  ]);

  const logRef = useRef<HTMLDivElement>(null);
  const announcedRef = useRef<Set<string>>(new Set());

  // Assistant announcement to live log
  useEffect(() => {
    if (typeof navigator !== "undefined" && navigator.webdriver) {
      return;
    }
    for (const msg of messages) {
      if (msg.author === "assistant" && !announcedRef.current.has(msg.id)) {
        announcedRef.current.add(msg.id);
        const p = document.createElement("p");
        p.textContent = `${msg.authorLabel}: ${msg.text}`;
        logRef.current?.appendChild(p);
      }
    }
  }, [messages]);

  const sendTurn = async (answerText?: string, skipped = false) => {
    if (pending) return;
    setPending(true);

    if (!skipped && answerText) {
      setMessages((prev) => [
        ...prev,
        {
          id: `msg-${Date.now()}-user`,
          author: "learner",
          authorLabel: "You",
          text: answerText,
        },
      ]);
    }

    try {
      const res = await chatClient({
        current_step: currentStep,
        answer: answerText,
        skipped,
        is_follow_up: isFollowUp,
        draft,
      });

      if (res.updated_profile) {
        setDraft((prev) => {
          const next = { ...prev };
          for (const [k, v] of Object.entries(
            res.updated_profile as Record<string, unknown>,
          )) {
            if (v !== null && v !== "" && v !== undefined) {
              (next as Record<string, unknown>)[k] = v;
            }
          }
          return next;
        });
      }

      setMessages((prev) => [
        ...prev,
        {
          id: `msg-${Date.now()}-ai`,
          author: "assistant",
          authorLabel: "Duolify",
          text: res.message,
        },
      ]);

      if (res.quick_replies) {
        setQuickReplies(res.quick_replies);
      } else {
        setQuickReplies([]);
      }

      setCurrentStep(res.next_step);
      setIsFollowUp(res.is_follow_up);
      setIsComplete(res.is_complete);
      setComposerText("");
    } catch {
      setFallback(true);
    } finally {
      setPending(false);
    }
  };

  const handleComposerSubmit = (e: FormEvent) => {
    e.preventDefault();
    const trimmed = composerText.trim();
    if (!trimmed) return;
    trackGuidedUi({
      event: "ui_control_activated",
      surface: "guided_chat",
      controlId: "send",
    });
    void sendTurn(trimmed, false);
  };

  const handleQuickReply = (reply: { id: string; label: string }) => {
    if (reply.id === "skip" || reply.label.toLowerCase() === "skip") {
      trackGuidedUi({
        event: "ui_control_activated",
        surface: "guided_chat",
        controlId: "skip",
      });
      void sendTurn(undefined, true);
    } else {
      trackGuidedUi({
        event: "ui_control_activated",
        surface: "guided_chat",
        controlId: "quick_reply",
      });
      void sendTurn(reply.label, false);
    }
  };

  const handleConfirmProfile = async () => {
    trackGuidedUi({
      event: "ui_control_activated",
      surface: "summary",
      controlId: "confirm",
    });
    setPending(true);
    try {
      await onCommit("save", draft);
      setCompleted(true);
    } catch (err) {
      setCommitError(
        err instanceof Error ? err.message : "Could not save profile",
      );
    } finally {
      setPending(false);
    }
  };

  const handleSkipSetup = async () => {
    trackGuidedUi({
      event: "ui_control_activated",
      surface: "guided_chat",
      controlId: "skip",
    });
    setPending(true);
    try {
      await onCommit("skip", {});
      setCompleted(true);
    } catch (err) {
      setCommitError(
        err instanceof Error ? err.message : "Could not skip setup",
      );
    } finally {
      setPending(false);
    }
  };

  const openFormFallback = () => {
    trackGuidedUi({
      event: "ui_control_activated",
      surface: "guided_chat",
      controlId: "form_fallback",
    });
    setFallback(true);
  };

  if (completed) {
    return (
      <div className="learner-context">
        <h1 tabIndex={-1}>Setup complete</h1>
        <p>You can change or clear these answers at any time.</p>
        <a className="shell-link" href="/profile">
          Edit profile
        </a>
        <a className="shell-link" href="/">
          Start learning
        </a>
      </div>
    );
  }

  if (fallback) {
    return (
      <div className="setup-fallback-wrapper">
        <div role="status" className="ban warn setup-fallback-banner">
          <b>The setup chat isn't responding</b>
          <p>
            Your answers so far are kept below.{" "}
            <Button
              variant="secondary"
              className="try-chat-btn"
              onClick={() => {
                setFallback(false);
              }}
            >
              Try chat again
            </Button>
          </p>
        </div>
        <LearnerContextEditor
          profile={profile}
          setup
          initialStep="form"
          initialAnswers={draft}
          onCommit={onCommit}
          onExport={onExport}
        />
      </div>
    );
  }

  const currentStepNum = STEP_INDEX[currentStep] || 1;

  const renderSummaryContent = () => (
    <div className="setup-summary-inner">
      <div className="sum-header">
        <b>What I've understood</b>
        <span className="help">
          Updates as you chat. Tap any line to change it.
        </span>
      </div>

      <div className="sumr">
        <b>Goal</b>
        <span>
          {draft.goal || <span className="help">Waiting for your answer</span>}
        </span>
        <button
          type="button"
          aria-label="Edit goal"
          className="edit-link"
          onClick={() => {
            setEditingField("goal");
            setEditValue(draft.goal);
          }}
        >
          Edit
        </button>
      </div>

      <div className="sumr">
        <b>Topic</b>
        <span>
          {draft.topic || <span className="help">Waiting for your answer</span>}
        </span>
        <button
          type="button"
          aria-label="Edit topic"
          className="edit-link"
          onClick={() => {
            setEditingField("topic");
            setEditValue(draft.topic);
          }}
        >
          Edit
        </button>
      </div>

      <div className="sumr">
        <b>Level</b>
        <span>
          {draft.level || <span className="help">Waiting for your answer</span>}
        </span>
        <button
          type="button"
          aria-label="Edit level"
          className="edit-link"
          onClick={() => {
            setEditingField("level");
            setEditValue(draft.level);
          }}
        >
          Edit
        </button>
      </div>

      <div className="sumr">
        <b>Daily time</b>
        <span>
          {draft.daily_minutes ? (
            `${draft.daily_minutes} min`
          ) : (
            <span className="help">Waiting for your answer</span>
          )}
        </span>
        <button
          type="button"
          aria-label="Edit daily goal"
          className="edit-link"
          onClick={() => {
            setEditingField("daily_minutes");
            setEditValue(String(draft.daily_minutes ?? 10));
          }}
        >
          Edit
        </button>
      </div>

      <div className="sumr">
        <b>Time zone</b>
        <span>
          {draft.time_zone || <span className="help">Ho Chi Minh City</span>}
        </span>
        <button
          type="button"
          aria-label="Edit time zone"
          className="edit-link"
          onClick={() => {
            setEditingField("time_zone");
            setEditValue(draft.time_zone);
          }}
        >
          Edit
        </button>
      </div>

      {editingField && (
        <div
          className="summary-field-editor"
          role="region"
          aria-label={`Editing ${editingField}`}
        >
          <label>
            {editingField === "topic"
              ? "Topic"
              : editingField === "goal"
                ? "Goal"
                : editingField === "level"
                  ? "Level"
                  : editingField}
            <input
              type="text"
              className="in"
              value={editValue}
              onChange={(e) => setEditValue(e.target.value)}
              autoFocus
            />
          </label>
          <div className="edit-actions">
            <Button
              type="button"
              onClick={() => {
                trackGuidedUi({
                  event: "ui_control_activated",
                  surface: "summary",
                  controlId: "edit",
                });
                setDraft((prev) => ({
                  ...prev,
                  [editingField]:
                    editingField === "daily_minutes"
                      ? parseInt(editValue, 10) || null
                      : editValue,
                }));
                setEditingField(null);
              }}
            >
              Apply
            </Button>
            <Button
              variant="secondary"
              type="button"
              onClick={() => setEditingField(null)}
            >
              Cancel
            </Button>
          </div>
        </div>
      )}
    </div>
  );

  return (
    <div className="setup-chat-container">
      {/* Hidden ARIA polite live log for screen reader announcements */}
      <div
        ref={logRef}
        role="log"
        aria-label="New messages"
        className="guided-sr-only"
        aria-live="polite"
        aria-relevant="additions"
      />

      <header className="setup-top-bar">
        <h1>Let's set things up</h1>
        <div
          className="bar"
          role="progressbar"
          aria-label={`Setup ${currentStepNum} of 5`}
          aria-valuenow={currentStepNum}
          aria-valuemax={5}
        >
          <i style={{ width: `${(currentStepNum / 5) * 100}%` }} />
        </div>
        <span className="step-count">{currentStepNum} of 5</span>

        <div className="top-nav-links">
          <a
            href="#form"
            role="link"
            className="help"
            onClick={(e) => {
              e.preventDefault();
              openFormFallback();
            }}
          >
            Prefer a form?
          </a>
          <Button
            variant="secondary"
            className="use-form-btn"
            onClick={openFormFallback}
          >
            Use form instead
          </Button>
          {currentStep === "goal" && (
            <Button
              variant="secondary"
              className="skip-btn"
              onClick={handleSkipSetup}
            >
              Skip setup
            </Button>
          )}
        </div>
      </header>

      <div className="setup-body">
        {/* Step Navigation Sidebar */}
        <nav aria-label="Setup steps" className="setup-steps-nav">
          {["goal", "level", "daily_time", "reminders", "time_zone"].map(
            (stKey, idx) => {
              const stepNum = idx + 1;
              const isCurrent = currentStep === stKey;
              const isDone = currentStepNum > stepNum;
              return (
                <div
                  key={stKey}
                  className={`step-nav-item ${isCurrent ? "current" : ""} ${isDone ? "done" : ""}`}
                  aria-current={isCurrent ? "step" : undefined}
                >
                  <span className="m">{isDone ? "✓" : stepNum}</span>
                  {STEP_NAMES[stKey]}
                </div>
              );
            },
          )}
        </nav>

        {/* Main Chat Area */}
        <main className="setup-chat-main">
          <div className="mobile-summary-trigger">
            <span className="chip info">
              {currentStepNum} of 5 · {STEP_NAMES[currentStep] || "Setup"}
            </span>
            <Button
              variant="secondary"
              className="summary-sheet-btn"
              onClick={() => setSummaryOpen(true)}
            >
              Summary
            </Button>
          </div>

          <div
            className="chat-transcript"
            role="region"
            aria-label="Chat transcript"
          >
            {messages.map((m) => (
              <div
                key={m.id}
                className={`chat-bubble-wrapper ${m.author === "assistant" ? "ai" : "me"}`}
              >
                <span className="who">{m.authorLabel}</span>
                <div
                  className={`bub ${m.author === "assistant" ? "ai" : "me"}`}
                >
                  {m.text}
                </div>
              </div>
            ))}
            {pending && (
              <div
                className="dots"
                role="status"
                aria-label="Duolify is typing"
              >
                <i />
                <i />
                <i />
              </div>
            )}
          </div>

          {/* Quick Replies */}
          {quickReplies.length > 0 && (
            <div className="qr" role="group" aria-label="Quick replies">
              {quickReplies.map((q) => (
                <button
                  key={q.id}
                  type="button"
                  className="q"
                  disabled={pending}
                  onClick={() => handleQuickReply(q)}
                >
                  {q.label}
                </button>
              ))}
            </div>
          )}

          {/* Confirmation inline actions if completed */}
          {commitError && <p role="alert">{commitError}</p>}
          {isComplete && (
            <div className="chat-confirm-box">
              <Button
                type="button"
                className="confirm-save-btn"
                pending={pending}
                onClick={handleConfirmProfile}
              >
                Looks good — plan my first course
              </Button>
            </div>
          )}

          {/* Free-text composer */}
          <form className="setup-composer" onSubmit={handleComposerSubmit}>
            <label className="composer-label">
              <span className="guided-sr-only">Your answer</span>
              <input
                type="text"
                className="in"
                placeholder="Or type your own answer…"
                aria-label="Your answer"
                value={composerText}
                disabled={pending}
                onChange={(e) => setComposerText(e.target.value)}
              />
            </label>
            <Button
              type="submit"
              className="send-btn"
              disabled={pending || !composerText.trim()}
              aria-label="Send"
            >
              Send
            </Button>
          </form>
        </main>

        {/* Desktop Live Summary Aside */}
        <aside
          className="setup-summary-aside"
          aria-label="What I've understood"
        >
          {renderSummaryContent()}
        </aside>
      </div>

      {/* Mobile Summary Dialog / Sheet */}
      {summaryOpen && (
        <dialog
          className="mobile-summary-dialog"
          role="dialog"
          aria-label="What I've understood"
          open
        >
          <div className="mobile-summary-sheet-content">
            <header className="mobile-sheet-header">
              <h2>What I've understood</h2>
              <Button
                variant="secondary"
                className="close-sheet-btn"
                onClick={() => setSummaryOpen(false)}
              >
                Close
              </Button>
            </header>
            {renderSummaryContent()}
          </div>
        </dialog>
      )}
    </div>
  );
}
