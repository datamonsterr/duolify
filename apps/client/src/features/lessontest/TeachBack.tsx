import { useEffect, useId, useRef, useState, type FormEvent } from "react";
import { AppLink } from "../../lib/navigation";
import { AuthError, session } from "../accounts/session";
import { useUiPreferences } from "../design/localization/Preferences";
import "./teach-back.css";

type Point = { text: string; slideIndex: number };

type Summary = {
  covered?: Point[] | null;
  missed?: Point[] | null;
  selfCheck?: boolean;
};
type Practice = {
  turn: number;
  followUp?: string;
  summary?: Summary;
};

function KeyPoints({
  title,
  points,
  lessonId,
}: {
  title: string;
  points: Point[];
  lessonId?: string;
}) {
  const { t } = useUiPreferences();
  return (
    <div>
      <h3>{title}</h3>
      {points.length ? (
        <ul>
          {points.map(({ text, slideIndex }) => (
            <li key={`${slideIndex}:${text}`}>
              {lessonId && slideIndex >= 0 ? (
                <AppLink
                  href={`/lesson/${encodeURIComponent(lessonId)}?slide=${slideIndex}&t=0`}
                >
                  {text}
                </AppLink>
              ) : (
                text
              )}
            </li>
          ))}
        </ul>
      ) : (
        <p>{t("teach_back.none")}</p>
      )}
    </div>
  );
}

export function TeachBack({
  courseId,
  lessonId,
}: {
  courseId: string;
  lessonId: string;
}) {
  const { t } = useUiPreferences();
  const id = useId();
  const path = `/v1/courses/${encodeURIComponent(courseId)}/lessons/${encodeURIComponent(lessonId)}/teach-back`;
  const [open, setOpen] = useState(false);
  const [practice, setPractice] = useState<Practice>();
  const [draft, setDraft] = useState("");
  const [sending, setSending] = useState(false);
  const [load, setLoad] = useState<"idle" | "loading" | "failed">("idle");
  const [sendFailed, setSendFailed] = useState(false);
  const panel = useRef<HTMLElement>(null);
  const summary = practice?.summary;

  useEffect(() => {
    if (open && summary) panel.current?.focus();
  }, [open, summary]);

  async function loadPractice() {
    setLoad("loading");
    try {
      setPractice(await session.request<Practice>(path));
      setLoad("idle");
    } catch {
      setLoad("failed");
    }
  }

  function toggle() {
    setOpen(!open);
    if (!open && !practice && load !== "loading") void loadPractice();
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const explanation = draft.trim();
    if (!practice || sending || !explanation) return;
    setSending(true);
    setSendFailed(false);
    try {
      const result = await session.request<Practice>(path, "POST", {
        turn: practice.turn,
        explanation,
      });
      setPractice(result);
      setDraft("");
    } catch (error) {
      if (error instanceof AuthError && error.status === 409) {
        const latest = await session.request<Practice>(path).catch(() => null);
        if (latest && latest.turn > practice.turn) {
          setPractice(latest);
          setDraft("");
          return;
        }
      }
      setSendFailed(true);
    } finally {
      setSending(false);
    }
  }

  return (
    <div className="teach-back">
      <button
        type="button"
        id={id}
        className="btn sec"
        aria-expanded={open}
        aria-controls={`${id}-practice`}
        onClick={toggle}
      >
        {t("teach_back.title")}
      </button>
      <section
        id={`${id}-practice`}
        aria-labelledby={id}
        ref={panel}
        tabIndex={-1}
        hidden={!open}
      >
        {open && load === "loading" && (
          <p role="status">{t("settings.loading")}</p>
        )}
        {open && load === "failed" && (
          <>
            <p role="alert">{t("teach_back.load_error")}</p>
            <button
              type="button"
              className="btn sec"
              onClick={() => void loadPractice()}
            >
              {t("settings.retry_load")}
            </button>
          </>
        )}
        {open && practice && !summary && (
          <form onSubmit={(event) => void submit(event)}>
            <p role="status">{practice.followUp ?? t("teach_back.prompt")}</p>
            <label htmlFor={`${id}-explanation`}>
              {t("teach_back.explanation")}
            </label>
            <textarea
              id={`${id}-explanation`}
              rows={4}
              maxLength={2000}
              value={draft}
              disabled={sending}
              onChange={(event) => setDraft(event.currentTarget.value)}
            />
            {sendFailed && <p role="alert">{t("teach_back.send_error")}</p>}
            <button
              type="submit"
              className="btn"
              disabled={sending || !draft.trim()}
            >
              {t(sending ? "teach_back.sending" : "teach_back.send")}
            </button>
          </form>
        )}
        {open && summary && (
          <div className="teach-back-summary">
            {summary.selfCheck ? (
              <KeyPoints
                title={t("teach_back.self_check")}
                points={[...(summary.covered ?? []), ...(summary.missed ?? [])]}
                lessonId={lessonId}
              />
            ) : (
              <>
                <KeyPoints
                  title={t("teach_back.covered")}
                  points={summary.covered ?? []}
                />
                <KeyPoints
                  title={t("teach_back.missed")}
                  points={summary.missed ?? []}
                  lessonId={lessonId}
                />
              </>
            )}
          </div>
        )}
      </section>
    </div>
  );
}
