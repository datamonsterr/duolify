import { useCallback, useEffect, useId, useRef, useState } from "react";
import {
  linkFailure,
  linkMessages,
  links,
  shortLink,
  type LinkCreated,
  type LinkFailure,
  type LinkState,
} from "./links";
import { InviteFriends } from "./InviteFriends";
import "./sharing.css";

/** Owner entry point on the course view. */
export function ShareCourse({ courseId }: { courseId: string }) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <button
        type="button"
        className="duo-button secondary"
        data-testid="share-course"
        onClick={() => setOpen(true)}
      >
        Share course
      </button>
      {open && (
        <ShareCoursePanel courseId={courseId} onClose={() => setOpen(false)} />
      )}
    </>
  );
}

type Busy = "checking" | "revoking" | "rotating" | null;
type Notice = "Link copied" | "Link replaced" | "Link revoked" | null;

/** Visibility and link controls. Desktop dialog, mobile full-height sheet. */
export function ShareCoursePanel({
  courseId,
  onClose,
}: {
  courseId: string;
  onClose: () => void;
}) {
  const id = useId();
  const dialog = useRef<HTMLDialogElement>(null);
  const close = useRef<HTMLButtonElement>(null);
  const [state, setState] = useState<LinkState | null>(null);
  const [link, setLink] = useState<LinkCreated | null>(null);
  const [version, setVersion] = useState<{ n: number; lessons: number } | null>(
    null,
  );
  const [busy, setBusy] = useState<Busy>(null);
  const [notice, setNotice] = useState<Notice>(null);
  const [failure, setFailure] = useState<LinkFailure | null>(null);
  const [verdictNote, setVerdictNote] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  const load = useCallback(() => {
    let active = true;
    links
      .status(courseId)
      .then((value) => {
        if (!active) return;
        setState(value);
        if (value.active)
          setVersion({
            n: value.version ?? 0,
            lessons: value.lesson_count ?? 0,
          });
      })
      .catch((error: unknown) => active && setFailure(linkFailure(error)))
      .finally(() => active && setLoading(false));
    return () => {
      active = false;
    };
  }, [courseId]);
  useEffect(() => load(), [load]);
  const retry = () => {
    setLoading(true);
    setFailure(null);
    load();
  };

  useEffect(() => {
    const node = dialog.current;
    if (!node) return;
    const opener = document.activeElement;
    if (typeof node.showModal === "function" && !node.open) node.showModal();
    else node.setAttribute("open", "");
    close.current?.focus();
    return () => {
      if (typeof node.close === "function" && node.open) node.close();
      if (opener instanceof HTMLElement && opener.isConnected) opener.focus();
    };
  }, []);

  async function run(work: Busy, task: () => Promise<void>) {
    setBusy(work);
    setFailure(null);
    setNotice(null);
    try {
      await task();
    } catch (error) {
      setFailure(linkFailure(error));
    } finally {
      setBusy(null);
    }
  }

  const publish = () =>
    run("checking", async () => {
      setVerdictNote(null);
      const result = await links.publish(courseId);
      if (result.kind === "blocked") {
        setVerdictNote(
          result.verdict === "block"
            ? "This version can't be published. Remove the flagged content and try again."
            : result.verdict === "pending_screening"
              ? "Screening is pending. Your course stays private. Learning is not affected."
              : "Needs a quick review. It stays private until review is available.",
        );
        return;
      }
      setLink(result.link);
      setVersion({ n: result.version, lessons: result.lessons });
      setState({ visibility: "link", active: true });
    });
  const rotate = () =>
    run("rotating", async () => {
      setLink(await links.rotate(courseId));
      setNotice("Link replaced");
    });
  const revoke = () =>
    run("revoking", async () => {
      await links.revoke(courseId);
      setLink(null);
      setState({ visibility: "private", active: false });
      setNotice("Link revoked");
    });
  async function copy() {
    if (!link) return;
    try {
      await navigator.clipboard.writeText(link.preview_url);
      setNotice("Link copied");
    } catch {
      setFailure("offline");
    }
  }

  const shared = state?.visibility === "link" && state.active;
  return (
    <dialog
      ref={dialog}
      className="sharing-sheet"
      aria-labelledby={`${id}-title`}
      data-testid="share-course-panel"
      onCancel={(event) => {
        event.preventDefault();
        onClose();
      }}
      onKeyDown={(event) => {
        if (event.key === "Escape") onClose();
        if (event.key !== "Tab") return;
        const items = [
          ...event.currentTarget.querySelectorAll<HTMLElement>(
            "button:not([disabled]), input:not([disabled]), a[href]",
          ),
        ];
        const first = items[0];
        const last = items[items.length - 1];
        if (!first || !last) return;
        if (event.shiftKey && document.activeElement === first) {
          event.preventDefault();
          last.focus();
        } else if (!event.shiftKey && document.activeElement === last) {
          event.preventDefault();
          first.focus();
        }
      }}
    >
      <div className="sharing-body">
        <header className="sharing-head">
          <h2 id={`${id}-title`}>Share course</h2>
          <button
            ref={close}
            type="button"
            className="duo-button quiet"
            onClick={onClose}
          >
            Close
          </button>
        </header>
        {loading && (
          <p role="status">
            <span className="sharing-spinner" aria-hidden="true" />
            Loading…
          </p>
        )}
        {failure && (
          <div className="sharing-error" role="alert">
            <p>{linkMessages[failure]}</p>
            {failure === "offline" && !state && (
              <button type="button" className="duo-button" onClick={retry}>
                Retry
              </button>
            )}
          </div>
        )}
        {verdictNote && (
          <p className="sharing-banner" role="alert">
            {verdictNote}
          </p>
        )}
        {state && (
          <fieldset className="sharing-visibility" disabled={busy !== null}>
            <legend>Who can see this course?</legend>
            <Choice
              group={`${id}-visibility`}
              label="Private"
              hint="Only you"
              checked={!shared}
              onSelect={() => shared && void revoke()}
            />
            <Choice
              group={`${id}-visibility`}
              label="Anyone with the link"
              hint="Not listed. Visitors see a preview and can sign in to enroll."
              checked={shared}
              onSelect={() => !shared && void publish()}
            />
            <Choice
              group={`${id}-visibility`}
              label="Public in marketplace"
              hint="Not available yet."
              checked={false}
              disabled
            />
          </fieldset>
        )}
        {busy === "checking" && (
          <p role="status">
            <span className="sharing-spinner" aria-hidden="true" />
            Checking this version for safety…
          </p>
        )}
        {shared && (
          <section className="sharing-link" aria-label="Share link">
            {link ? (
              <>
                <code data-testid="share-link-url">
                  {shortLink(link.preview_url)}
                </code>
                <button type="button" className="duo-button" onClick={copy}>
                  Copy link
                </button>
              </>
            ) : (
              <p className="sharing-note">
                The full link is shown only when it is created. Use New link to
                get a fresh one to copy.
              </p>
            )}
            {version && version.n > 0 && (
              <p className="sharing-note">
                Version {version.n} · {version.lessons} lessons ready. Learners
                pay for lessons beyond what you've generated.
              </p>
            )}
            <div className="sharing-actions">
              <button
                type="button"
                className="duo-button secondary"
                disabled={busy !== null}
                onClick={() => void rotate()}
              >
                New link
              </button>
              <button
                type="button"
                className="duo-button secondary"
                disabled={busy !== null}
                onClick={() => void revoke()}
              >
                Revoke link
              </button>
            </div>
          </section>
        )}
        {state && (
          <InviteFriends
            courseId={courseId}
            shared={shared}
            onShareFirst={() => void publish()}
          />
        )}
        <p className="sharing-note">
          Never shared: your profile, notes, chat, progress or steering
          instructions.
        </p>
        <p className="sharing-note" role="status" aria-live="polite">
          {notice}
        </p>
      </div>
    </dialog>
  );
}

function Choice({
  group,
  label,
  hint,
  checked,
  disabled,
  onSelect,
}: {
  group: string;
  label: string;
  hint: string;
  checked: boolean;
  disabled?: boolean;
  onSelect?: () => void;
}) {
  const id = useId();
  return (
    <div className="sharing-choice">
      <input
        id={id}
        type="radio"
        name={group}
        checked={checked}
        disabled={disabled}
        aria-describedby={`${id}-hint`}
        onChange={onSelect}
      />
      <label htmlFor={id}>
        <strong>{label}</strong>
      </label>
      <small id={`${id}-hint`}>{hint}</small>
    </div>
  );
}
