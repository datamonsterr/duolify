import { useCallback, useEffect, useId, useRef, useState } from "react";
import {
  copyUpdates,
  updateFailure,
  updateMessages,
  type UpdateFailure,
  type UpdateInfo,
} from "./republish";
import { ArchivedLessons } from "./ArchivedLessons";
import "./sharing.css";

const plural = (n: number) =>
  n === 1 ? "1 completed lesson stays" : `${n} completed lessons stay`;

/**
 * Learner entry point on a pinned copy: nothing unless a newer version exists. A forked copy only
 * explains why it gets no updates.
 */
export function CourseUpdate({
  courseId,
  onUpdated,
}: {
  courseId: string;
  onUpdated: () => void;
}) {
  const [info, setInfo] = useState<UpdateInfo | null>(null);
  const [open, setOpen] = useState(false);
  const opener = useRef<HTMLButtonElement>(null);

  const load = useCallback(async () => {
    try {
      setInfo(await copyUpdates.info(courseId));
    } catch {
      setInfo(null); // the offer is optional: the course keeps working
    }
  }, [courseId]);
  useEffect(() => {
    let active = true;
    copyUpdates
      .info(courseId)
      .then((value) => active && setInfo(value))
      .catch(() => active && setInfo(null));
    return () => {
      active = false;
    };
  }, [courseId]);

  if (!info) return null;
  const history = (
    <ArchivedLessons courseId={courseId} count={info.archived_count} />
  );
  if (info.forked)
    return (
      <>
        <p className="muted" data-testid="update-forked">
          You've personalized this course, so it no longer receives updates.
        </p>
        {history}
      </>
    );
  if (!info.update_available) return history;
  return (
    <>
      {history}
      <button
        ref={opener}
        type="button"
        className="duo-button secondary"
        onClick={() => setOpen(true)}
      >
        Update available
      </button>
      {open && (
        <UpdateSheet
          courseId={courseId}
          info={info}
          reload={load}
          onClose={() => {
            setOpen(false);
            queueMicrotask(() => opener.current?.focus());
          }}
          onUpdated={() => {
            setOpen(false);
            onUpdated();
            void load();
          }}
        />
      )}
    </>
  );
}

function UpdateSheet({
  courseId,
  info,
  reload,
  onClose,
  onUpdated,
}: {
  courseId: string;
  info: UpdateInfo;
  reload: () => Promise<void>;
  onClose: () => void;
  onUpdated: () => void;
}) {
  const id = useId();
  const dialog = useRef<HTMLDialogElement>(null);
  const first = useRef<HTMLButtonElement>(null);
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<UpdateFailure | null>(null);

  useEffect(() => {
    const node = dialog.current;
    if (!node) return;
    if (typeof node.showModal === "function" && !node.open) node.showModal();
    else node.setAttribute("open", "");
    first.current?.focus();
    return () => {
      if (typeof node.close === "function" && node.open) node.close();
    };
  }, []);

  async function apply() {
    setBusy(true);
    setFailure(null);
    try {
      await copyUpdates.apply(courseId, {
        lock_version: info.lock_version,
        to_version: info.latest_version,
      });
      onUpdated();
    } catch (error) {
      const kind = updateFailure(error);
      setFailure(kind);
      if (kind === "stale") await reload();
    } finally {
      setBusy(false);
    }
  }

  return (
    <dialog
      ref={dialog}
      className="sharing-sheet"
      aria-labelledby={`${id}-title`}
      data-testid="course-update"
      onCancel={(event) => {
        event.preventDefault();
        onClose();
      }}
      onKeyDown={(event) => {
        if (event.key === "Escape") onClose();
        if (event.key !== "Tab") return;
        const items = [
          ...event.currentTarget.querySelectorAll<HTMLElement>(
            "button:not([disabled])",
          ),
        ];
        const head = items[0];
        const last = items[items.length - 1];
        if (!head || !last) return;
        if (event.shiftKey && document.activeElement === head) {
          event.preventDefault();
          last.focus();
        } else if (!event.shiftKey && document.activeElement === last) {
          event.preventDefault();
          head.focus();
        }
      }}
    >
      <div className="sharing-body">
        <header className="sharing-head">
          <div>
            <p className="sharing-note">Course update</p>
            <h2 id={`${id}-title`}>
              Version {info.latest_version} is available
            </h2>
          </div>
        </header>
        <p>You're on version {info.current_version}.</p>
        <section aria-label="What's new">
          <h3>What's new</h3>
          {info.changelog.length === 0 ? (
            <p className="sharing-note">The owner didn't add a changelog.</p>
          ) : (
            <ul className="sharing-changelog">
              {info.changelog.map((entry) => (
                <li key={entry.version}>
                  {entry.text && <p>{entry.text}</p>}
                  <p className="sharing-note">
                    {entry.added} added · {entry.removed} removed ·{" "}
                    {entry.retitled} renamed
                  </p>
                </li>
              ))}
            </ul>
          )}
        </section>
        <section aria-label="What happens to your progress">
          <h3>What happens to your progress</h3>
          <p>
            Your {plural(info.completed_lessons)} completed. Lessons removed
            upstream stay in your history as archived.
          </p>
        </section>
        {failure && (
          <div className="sharing-error" role="alert">
            <p>{updateMessages[failure]}</p>
            {failure === "offline" && (
              <button
                type="button"
                className="duo-button"
                onClick={() => void apply()}
              >
                Retry
              </button>
            )}
          </div>
        )}
        <div className="sharing-actions">
          <button
            ref={first}
            type="button"
            className="duo-button"
            disabled={busy}
            onClick={() => void apply()}
          >
            Update to version {info.latest_version}
          </button>
          <button
            type="button"
            className="duo-button quiet"
            disabled={busy}
            onClick={onClose}
          >
            Stay on version {info.current_version}
          </button>
        </div>
        {busy && (
          <p role="status">
            <span className="sharing-spinner" aria-hidden="true" />
            Updating…
          </p>
        )}
      </div>
    </dialog>
  );
}
