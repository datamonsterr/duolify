import {
  useEffect,
  useRef,
  useState,
  type KeyboardEvent,
  type ReactNode,
} from "react";
import { useUiPreferences } from "../design/localization/Preferences";
import type { LessonRef, ReplyClass, TutorService } from "./tutor";
import { trackTutorRecovery } from "../../lib/analytics";
import { tutorService } from "./tutor";
import { lastIndex, useTutor, type ChatMessage } from "./useTutor";
import "./tutor.css";

const starters = [
  "tutor.starter_1",
  "tutor.starter_2",
  "tutor.starter_3",
] as const;

function ConfirmDelete({
  onCancel,
  onConfirm,
  busy,
  error,
}: {
  onCancel: () => void;
  onConfirm: () => void;
  busy: boolean;
  error: boolean;
}) {
  const { t } = useUiPreferences();
  const cancel = useRef<HTMLButtonElement>(null);
  const confirm = useRef<HTMLButtonElement>(null);
  useEffect(() => cancel.current?.focus(), []);
  function onKeyDown(event: KeyboardEvent) {
    if (event.key === "Escape") {
      event.stopPropagation();
      onCancel();
    } else if (event.key === "Tab") {
      // Two controls: keep focus inside the modal dialog.
      const first = cancel.current;
      const last = confirm.current;
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last?.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first?.focus();
      }
    }
  }
  return (
    <div className="tutor-scrim">
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="tutor-delete-title"
        className="tutor-dialog"
        onKeyDown={onKeyDown}
      >
        <b id="tutor-delete-title">{t("tutor.delete_title")}</b>
        <p>{t("tutor.delete_body")}</p>
        {error && <p role="alert">{t("tutor.delete_error")}</p>}
        <div className="tutor-row">
          <button
            type="button"
            ref={cancel}
            className="btn sec"
            onClick={onCancel}
          >
            {t("tutor.delete_cancel")}
          </button>
          <button
            type="button"
            ref={confirm}
            className="btn tutor-danger"
            disabled={busy}
            onClick={onConfirm}
          >
            {t("tutor.delete_confirm")}
          </button>
        </div>
      </div>
    </div>
  );
}

function Bubble({
  message,
  showRetry,
  onRetry,
  canRetry,
}: {
  message: ChatMessage;
  showRetry: boolean;
  canRetry: boolean;
  onRetry: () => void;
}) {
  const { t } = useUiPreferences();
  const mine = message.role === "user";
  return (
    <div className={`tutor-msg ${mine ? "me" : "ai"}`}>
      <span className="tutor-who">{t(mine ? "tutor.you" : "tutor.tutor")}</span>
      {(message.content || mine) && (
        <div className="tutor-bubble" dir="auto">
          {message.content}
        </div>
      )}
      {message.status === "stopped" && (
        <span className="tutor-who">{t("tutor.stopped_tag")}</span>
      )}
      {message.status === "interrupted" && (
        <div className="tutor-note warn">
          <div>
            <b>{t("tutor.interrupted_title")}</b>
            <p>
              {t(
                message.reason === "idle_timeout"
                  ? "tutor.idle_body"
                  : "tutor.interrupted_body",
              )}
            </p>
            {showRetry && (
              <button
                type="button"
                className="btn sec tutor-small"
                disabled={!canRetry}
                onClick={onRetry}
              >
                {t("tutor.ask_again")}
              </button>
            )}
          </div>
        </div>
      )}
    </div>
  );
}

function Shell({
  embedded,
  label,
  onKeyDown,
  children,
}: {
  embedded: boolean;
  label: string;
  onKeyDown: (event: KeyboardEvent) => void;
  children: ReactNode;
}) {
  return embedded ? (
    <section
      className="tutor-panel tutor-embedded"
      aria-label={label}
      onKeyDown={onKeyDown}
    >
      {children}
    </section>
  ) : (
    <aside className="tutor-panel" aria-label={label} onKeyDown={onKeyDown}>
      {children}
    </aside>
  );
}

export function TutorChat({
  courseId,
  service = tutorService,
  lesson,
  embedded = false,
  onClose,
  onReadTranscript,
}: {
  courseId: string;
  service?: TutorService;
  /** Reference to the lesson the Learner paused; the API verifies it and builds the context. */
  lesson?: LessonRef;
  /** Rendered inside the lesson sheet: always open, no launcher, the sheet owns the close control. */
  embedded?: boolean;
  onClose?: () => void;
  onReadTranscript?: () => void;
}) {
  const { t, number } = useUiPreferences();
  const [launched, setOpen] = useState(false);
  const open = embedded || launched;
  const recover = (
    action: "retry" | "reload" | "top_up" | "read_transcript",
  ) => {
    if (lesson) trackTutorRecovery(action);
  };
  const [context, setContext] = useState<boolean>();
  const [confirming, setConfirming] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState(false);
  const tutor = useTutor(courseId, service, open, lesson);
  const entry = useRef<HTMLButtonElement>(null);
  const field = useRef<HTMLTextAreaElement>(null);
  const log = useRef<HTMLDivElement>(null);
  const deleteButton = useRef<HTMLButtonElement>(null);
  const wasOpen = useRef(false);
  const wasBusy = useRef(false);
  const busy = tutor.phase !== "idle";
  const streaming = tutor.phase === "streaming";
  const showContext = context ?? tutor.messages.length === 0;

  useEffect(() => {
    // Embedded: the sheet records its opener in its own effect, so focus the composer afterwards.
    if (open) {
      if (embedded) queueMicrotask(() => field.current?.focus());
      else field.current?.focus();
    } else if (wasOpen.current) entry.current?.focus();
    wasOpen.current = open;
  }, [open, embedded]);
  // Start and finish are announced once; the streaming log itself stays silent.
  const lastReply = tutor.messages[tutor.messages.length - 1];
  const [announce, setAnnounce] = useState<"" | "answering" | "replied">("");
  const [turnStart, setTurnStart] = useState<{
    busy: boolean;
    lastId?: string;
  }>({ busy: false });
  if (busy !== turnStart.busy) {
    setTurnStart({ busy, lastId: busy ? lastReply?.id : turnStart.lastId });
    setAnnounce(
      busy
        ? "answering"
        : lastReply?.role === "assistant" &&
            lastReply.status === "complete" &&
            lastReply.id !== turnStart.lastId
          ? "replied"
          : "",
    );
  }
  // Return to the composer when a turn ends; nothing moves focus while streaming.
  useEffect(() => {
    if (wasBusy.current && !busy && open) field.current?.focus();
    wasBusy.current = busy;
  }, [busy, open]);
  useEffect(() => {
    const node = log.current;
    if (node) node.scrollTop = node.scrollHeight;
  }, [tutor.messages, tutor.notice]);

  const lastInterrupted = lastIndex(
    tutor.messages,
    (m) => m.status === "interrupted",
  );
  const text = tutor.draft.trim();
  const canSend = !!text && !busy && tutor.online && !tutor.block;
  const notice = tutor.notice;
  const transcript = onReadTranscript ? (
    <button
      type="button"
      className="btn sec tutor-small"
      onClick={() => {
        recover("read_transcript");
        onReadTranscript();
      }}
    >
      {t("tutor.read_transcript")}
    </button>
  ) : null;

  function onKeyDown(event: KeyboardEvent<HTMLTextAreaElement>) {
    if (
      event.key === "Enter" &&
      !event.shiftKey &&
      !event.nativeEvent.isComposing
    ) {
      event.preventDefault();
      if (canSend) tutor.send();
    }
  }
  async function confirmDelete() {
    setDeleting(true);
    setDeleteError(false);
    try {
      await tutor.remove();
      setConfirming(false);
      setContext(undefined);
      field.current?.focus();
    } catch {
      setDeleteError(true);
    } finally {
      setDeleting(false);
    }
  }
  function cancelDelete() {
    setConfirming(false);
    setDeleteError(false);
    deleteButton.current?.focus();
  }
  const daily = tutor.wallet?.daily_free.available;
  const paid = tutor.wallet
    ? tutor.wallet.purchased + tutor.wallet.subscription
    : undefined;

  return (
    <>
      {!embedded && (
        <button
          type="button"
          ref={entry}
          className="btn sec tutor-entry"
          aria-expanded={open}
          onClick={() => setOpen(true)}
        >
          {t("tutor.entry")}
        </button>
      )}
      {open && (
        <Shell
          embedded={embedded}
          label={t("tutor.region")}
          onKeyDown={(event) => {
            if (event.key === "Escape" && !event.defaultPrevented) {
              if (embedded) onClose?.();
              else setOpen(false);
            }
          }}
        >
          <header className="tutor-top">
            <h2>{t("tutor.title")}</h2>
            <button
              type="button"
              className="tutor-icon"
              aria-label={t("tutor.context_toggle")}
              aria-expanded={showContext}
              onClick={() => setContext(!showContext)}
            >
              <span aria-hidden="true">i</span>
            </button>
            {tutor.threadId && (
              <button
                type="button"
                ref={deleteButton}
                className="tutor-icon"
                aria-label={t("tutor.delete")}
                disabled={busy}
                onClick={() => setConfirming(true)}
              >
                <span aria-hidden="true">⋯</span>
              </button>
            )}
            {!embedded && (
              <button
                type="button"
                className="tutor-icon"
                aria-label={t("tutor.close")}
                onClick={() => setOpen(false)}
              >
                <span aria-hidden="true">×</span>
              </button>
            )}
          </header>
          {showContext && (
            <div className="tutor-ctx">
              <b>{t("tutor.context_title")}</b>
              <span>
                {t(lesson ? "tutor.lesson_context" : "tutor.context_body")}
              </span>
              <span>{t("tutor.context_not")}</span>
            </div>
          )}
          <div className="tutor-sr" role="status">
            {announce === "answering"
              ? t("tutor.answering")
              : announce === "replied"
                ? t("tutor.replied")
                : ""}
          </div>
          {!tutor.online && (
            <div className="tutor-note warn" role="status">
              <div>
                <b>{t("tutor.offline_title")}</b>
                <p>{t("tutor.offline_body")}</p>
                {transcript}
              </div>
            </div>
          )}
          <div
            className="tutor-log"
            ref={log}
            role="log"
            aria-live={streaming ? "off" : "polite"}
            aria-busy={streaming}
            aria-label={t("tutor.title")}
          >
            {tutor.loading && <p role="status">{t("tutor.loading")}</p>}
            {!tutor.loading &&
              tutor.messages.length === 0 &&
              notice?.kind !== "load" && (
                <div className="tutor-empty">
                  <b>{t("tutor.empty_title")}</b>
                  <div className="tutor-starters">
                    {starters.map((id) => (
                      <button
                        type="button"
                        key={id}
                        className="tutor-starter"
                        onClick={() => {
                          tutor.setDraft(t(id));
                          field.current?.focus();
                        }}
                      >
                        {t(id)}
                      </button>
                    ))}
                  </div>
                </div>
              )}
            {tutor.messages.map((message, index) => (
              <Bubble
                key={message.id}
                message={message}
                showRetry={index === lastInterrupted}
                canRetry={!busy && tutor.online}
                onRetry={() => {
                  recover("retry");
                  tutor.askAgain();
                }}
              />
            ))}
            {streaming && (
              <span className="tutor-chip">{t("tutor.writing")}</span>
            )}
          </div>
          {notice?.kind === "load" && (
            <div className="tutor-note bad" role="alert">
              <div>
                <b>{t("tutor.load_error")}</b>
                <button
                  type="button"
                  className="btn sec tutor-small"
                  onClick={() => {
                    recover("reload");
                    tutor.reload();
                  }}
                >
                  {t("tutor.reload")}
                </button>
                {transcript}
              </div>
            </div>
          )}
          {notice?.kind === "credits" && (
            <div className="tutor-note warn" role="alert">
              <div>
                <b>{t("tutor.credits_title")}</b>
                <p>{t("tutor.credits_body")}</p>
                {notice.nextGrantAt && (
                  <p>
                    {t("tutor.credits_next", {
                      time: new Date(notice.nextGrantAt).toLocaleString(),
                    })}
                  </p>
                )}
                <a
                  className="btn tutor-small"
                  href="/credits"
                  onClick={() => recover("top_up")}
                >
                  {t("tutor.credits_link")}
                </a>
              </div>
            </div>
          )}
          {notice?.kind === "failed" && (
            <div className="tutor-note bad" role="alert">
              <div>
                <b>{t("tutor.failed_title")}</b>
                <p>{t("tutor.failed_body")}</p>
                <button
                  type="button"
                  className="btn sec tutor-small"
                  disabled={!canSend}
                  onClick={() => {
                    recover("retry");
                    tutor.send();
                  }}
                >
                  {t("tutor.retry")}
                </button>
                {transcript}
              </div>
            </div>
          )}
          {tutor.block?.kind === "approval" && (
            <div className="tutor-note warn" role="alert">
              <div>
                <b>{t("tutor.approval_title")}</b>
                <p>
                  {t("tutor.approval_body", {
                    tokens: number(tutor.block.required ?? 0),
                    limit: number(tutor.block.limit ?? 0),
                  })}
                </p>
                <button
                  type="button"
                  className="btn tutor-small"
                  disabled={busy}
                  onClick={() => {
                    tutor.approve();
                    field.current?.focus();
                  }}
                >
                  {t("tutor.approval_action")}
                </button>
              </div>
            </div>
          )}
          {tutor.block?.kind === "too_large" && (
            <div className="tutor-note bad" role="alert">
              <div>
                <b>{t("tutor.too_large_title")}</b>
                <p>{t("tutor.too_large_body")}</p>
              </div>
            </div>
          )}
          {notice?.kind === "stale" && (
            <div className="tutor-note info" role="status">
              <p>{t("tutor.stale")}</p>
            </div>
          )}
          {notice?.kind === "stopped" && (
            <div className="tutor-note info" role="status">
              <div>
                <b>{t("tutor.stopped_title")}</b>
                <p>{t("tutor.stopped_body")}</p>
              </div>
            </div>
          )}
          <form
            className="tutor-composer"
            onSubmit={(event) => {
              event.preventDefault();
              if (canSend) tutor.send();
            }}
          >
            <fieldset className="tutor-class" disabled={busy}>
              <legend className="tutor-sr">{t("tutor.class_label")}</legend>
              {(["short", "long"] as ReplyClass[]).map((value) => (
                <label key={value} className={tutor.cls === value ? "on" : ""}>
                  <input
                    type="radio"
                    name="tutor-class"
                    value={value}
                    checked={tutor.cls === value}
                    onChange={() => tutor.setCls(value)}
                  />
                  {t(
                    value === "short"
                      ? "tutor.class_short"
                      : "tutor.class_long",
                  )}
                </label>
              ))}
            </fieldset>
            <div className="tutor-input">
              <label>
                <span className="tutor-sr">{t("tutor.message_label")}</span>
                <textarea
                  ref={field}
                  rows={2}
                  value={tutor.draft}
                  disabled={busy}
                  maxLength={4000}
                  dir="auto"
                  onChange={(event) => tutor.setDraft(event.target.value)}
                  onKeyDown={onKeyDown}
                />
              </label>
              {busy ? (
                <button
                  type="button"
                  className="tutor-send"
                  aria-label={t("tutor.stop")}
                  onClick={() => void tutor.stop()}
                >
                  <span aria-hidden="true">■</span>
                </button>
              ) : (
                <button
                  type="submit"
                  className="tutor-send"
                  aria-label={t(
                    tutor.online ? "tutor.send" : "tutor.send_offline",
                  )}
                  disabled={!canSend}
                >
                  <span aria-hidden="true">➤</span>
                </button>
              )}
            </div>
            <div className="tutor-help" aria-live="polite">
              <span>
                {tutor.quote
                  ? t("tutor.estimate", {
                      reply: t(
                        tutor.cls === "short"
                          ? "tutor.class_short"
                          : "tutor.class_long",
                      ),
                      credits: number(tutor.quote.quote.content_ceiling),
                    })
                  : tutor.quoting && text
                    ? t("tutor.estimate_loading")
                    : t("tutor.estimate_hint")}
              </span>
              {daily !== undefined && paid !== undefined && (
                <span>
                  {t("tutor.balance", {
                    daily: number(daily),
                    paid: number(paid),
                  })}
                </span>
              )}
            </div>
          </form>
          {confirming && (
            <ConfirmDelete
              busy={deleting}
              error={deleteError}
              onCancel={cancelDelete}
              onConfirm={() => void confirmDelete()}
            />
          )}
        </Shell>
      )}
    </>
  );
}
