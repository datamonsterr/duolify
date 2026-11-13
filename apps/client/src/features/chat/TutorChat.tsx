import {
  useEffect,
  useRef,
  useState,
  type KeyboardEvent,
  type ReactNode,
} from "react";
import { useUiPreferences } from "../design/localization/Preferences";
import type { LessonRef, ReplyClass, TutorService } from "./tutor";
import { trackTutorControl, trackTutorRecovery } from "../../lib/analytics";
import { tutorService } from "./tutor";
import {
  lastIndex,
  useTutor,
  type ChatMessage,
  type SaveState,
} from "./useTutor";
import "./tutor.css";

/** First question of the thread, clipped for the delete confirmation. */
function threadTitle(messages: ChatMessage[]) {
  const first = (messages.find((m) => m.role === "user")?.content ?? "")
    .replace(/\s+/g, " ")
    .trim();
  const chars = [...first];
  return chars.length > 28
    ? `${chars.slice(0, 28).join("").trimEnd()}\u2026`
    : first;
}

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
  title,
  count,
}: {
  onCancel: () => void;
  onConfirm: () => void;
  busy: boolean;
  error: boolean;
  /** Names the thread so the Learner knows which chat goes; shown on this device only. */
  title: string;
  count: number;
}) {
  const { t } = useUiPreferences();
  const cancel = useRef<HTMLButtonElement>(null);
  const confirm = useRef<HTMLButtonElement>(null);
  const dialog = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    // A nested modal <dialog> is the topmost layer, so the browser sends Escape's close request
    // here and not to the modal sheet behind it.
    const node = dialog.current;
    if (node && !node.open) {
      if (typeof node.showModal === "function") node.showModal();
      else node.setAttribute("open", ""); // Non-browser platform doubles.
    }
    cancel.current?.focus();
  }, []);
  function onKeyDown(event: KeyboardEvent) {
    if (event.key === "Escape") {
      // Cancel only this dialog, never the sheet behind it.
      event.preventDefault();
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
    <dialog
      ref={dialog}
      aria-labelledby="tutor-delete-title"
      className="tutor-dialog"
      onKeyDown={onKeyDown}
      onCancel={(event) => {
        event.preventDefault();
        onCancel();
      }}
    >
      <b id="tutor-delete-title">{t("tutor.delete_title")}</b>
      <p>{t("tutor.delete_body", { title, count })}</p>
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
    </dialog>
  );
}

function Bubble({
  message,
  showRetry,
  onRetry,
  canRetry,
  save,
  canSave,
  onSave,
}: {
  message: ChatMessage;
  showRetry: boolean;
  canRetry: boolean;
  onRetry: () => void;
  save?: SaveState;
  canSave: boolean;
  onSave: () => void;
}) {
  const { t } = useUiPreferences();
  const mine = message.role === "user";
  // Only a stored, finished reply can become a note: not a streaming or locally pending one.
  const savable =
    !mine &&
    message.content !== "" &&
    (message.status === "complete" || message.status === "stopped") &&
    !message.id.startsWith("local-");
  return (
    <div className={`tutor-msg ${mine ? "me" : "ai"}`}>
      <span className="tutor-who">
        {mine ? (
          t("tutor.you")
        ) : message.tutor?.name ? (
          <bdi className="tutor-name">{message.tutor.name}</bdi>
        ) : (
          t("tutor.tutor")
        )}
      </span>
      {!mine && message.tutor?.fallback && (
        <div className="tutor-note info" role="status">
          <p>{t("tutor.fallback_notice")}</p>
        </div>
      )}
      {(message.content || mine) && (
        <div className="tutor-bubble" dir="auto">
          {message.content}
        </div>
      )}
      {message.status === "stopped" && (
        <span className="tutor-who">{t("tutor.stopped_tag")}</span>
      )}
      {savable && save?.state !== "saved" && (
        <>
          <button
            type="button"
            className="btn sec tutor-small tutor-save"
            disabled={!canSave || save?.state === "saving"}
            onClick={onSave}
          >
            {t(save?.state === "saving" ? "tutor.saving" : "tutor.save")}
          </button>
          {save?.state === "error" && (
            <div className="tutor-note bad" role="alert">
              <p>{t("tutor.save_error")}</p>
            </div>
          )}
        </>
      )}
      {savable && save?.state === "saved" && (
        <div className="tutor-note info" role="status">
          <div>
            <p>{t(save.pending ? "tutor.saved_pending" : "tutor.saved")}</p>
            {save.truncated && <p>{t("tutor.saved_truncated")}</p>}
            <a className="btn sec tutor-small" href="/notes">
              {t("tutor.saved_open")}
            </a>
          </div>
        </div>
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

function TutorPicker({ tutor }: { tutor: ReturnType<typeof useTutor> }) {
  const { t } = useUiPreferences();
  const [open, setOpen] = useState(false);
  const opener = useRef<HTMLButtonElement>(null);
  const group = useRef<HTMLDivElement>(null);
  const roster = tutor.roster;
  useEffect(() => {
    if (open)
      group.current
        ?.querySelector<HTMLInputElement>("input:checked:not(:disabled)")
        ?.focus();
  }, [open]);
  if (roster.state === "none") return null;
  if (roster.state === "loading")
    return (
      <p className="tutor-pick-note" role="status">
        {t("tutor.picker_loading")}
      </p>
    );
  if (roster.state === "error")
    return (
      <div className="tutor-note bad" role="status">
        <div>
          <b>{t("tutor.picker_error")}</b>
          <button
            type="button"
            className="btn sec tutor-small"
            onClick={() => tutor.reloadPersonas()}
          >
            {t("tutor.picker_retry")}
          </button>
        </div>
      </div>
    );
  const { tutors, cast_source: source } = roster.roster;
  const current =
    tutors.find((x) => x.speaker_key === tutor.speaker && x.available) ??
    tutors.find((x) => x.speaker_key === roster.roster.default_speaker) ??
    tutors[0];
  const close = () => {
    setOpen(false);
    opener.current?.focus();
  };
  return (
    <div className="tutor-pick">
      <button
        type="button"
        ref={opener}
        className="btn sec tutor-small tutor-pick-open"
        aria-expanded={open}
        onClick={() => {
          if (!open) tutor.reloadPersonas(true);
          setOpen(!open);
        }}
      >
        {t("tutor.picker_prefix")}{" "}
        <bdi className="tutor-pick-name">{current?.name}</bdi>
      </button>
      {open && (
        <div
          className="tutor-pick-sheet"
          ref={group}
          role="radiogroup"
          aria-label={t("tutor.picker_label")}
          onKeyDown={(event) => {
            if (event.key === "Escape") {
              event.preventDefault();
              close();
            }
          }}
        >
          <span className="tutor-who">
            {t(
              source === "lesson"
                ? "tutor.source_lesson"
                : source === "course"
                  ? "tutor.source_course"
                  : "tutor.source_default",
            )}
          </span>
          {tutors.map((item) => (
            <label
              key={item.speaker_key}
              className={`tutor-pick-row${tutor.speaker === item.speaker_key ? " on" : ""}`}
            >
              <input
                type="radio"
                name="tutor-speaker"
                value={item.speaker_key}
                checked={current?.speaker_key === item.speaker_key}
                disabled={!item.available}
                onChange={() => tutor.setSpeaker(item.speaker_key)}
              />
              <bdi className="tutor-pick-name">{item.name}</bdi>
              <span className="tutor-pick-role">
                {t(
                  item.role === "guide"
                    ? "tutor.role_guide"
                    : "tutor.role_supporter",
                )}
                {!item.available && ` \u00b7 ${t("tutor.picker_unavailable")}`}
              </span>
            </label>
          ))}
          {tutors.length <= 1 && (
            <p className="tutor-pick-note">{t("tutor.picker_only_guide")}</p>
          )}
          <p className="tutor-pick-note">{t("tutor.picker_next")}</p>
          <button type="button" className="btn tutor-small" onClick={close}>
            {t("tutor.picker_done")}
          </button>
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
  // Focus can only move once the nested modal is gone (everything behind it is inert): back to
  // Delete when the chat remains, otherwise to the composer.
  const wasConfirming = useRef(false);
  useEffect(() => {
    if (wasConfirming.current && !confirming)
      (deleteButton.current ?? field.current)?.focus();
    wasConfirming.current = confirming;
  }, [confirming]);
  async function confirmDelete() {
    trackTutorControl("delete_thread");
    setDeleting(true);
    setDeleteError(false);
    try {
      await tutor.remove();
      setConfirming(false);
      setContext(undefined);
    } catch {
      setDeleteError(true);
    } finally {
      setDeleting(false);
    }
  }
  function cancelDelete() {
    setConfirming(false);
    setDeleteError(false);
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
                disabled={busy || !tutor.online}
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
          <TutorPicker tutor={tutor} />
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
                save={tutor.saves[message.id]}
                canSave={tutor.online}
                onSave={() => void tutor.saveReply(message.id)}
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
              title={threadTitle(tutor.messages)}
              count={tutor.messages.length}
              onCancel={cancelDelete}
              onConfirm={() => void confirmDelete()}
            />
          )}
        </Shell>
      )}
    </>
  );
}
