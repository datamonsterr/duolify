import {
  Suspense,
  lazy,
  useCallback,
  useEffect,
  useRef,
  useState,
} from "react";
import { useUiPreferences } from "../design/localization/Preferences";
import { session } from "../accounts/session";
import { marketFailure, marketMessages } from "./market";
import { ReportSheet } from "./ReportSheet";
import { guidedEnabled } from "../guided-chat/flags";

const GuidedReview = lazy(() =>
  import("./GuidedReview").then((m) => ({ default: m.GuidedReview })),
);

type Review = {
  id: string;
  stars: number;
  text: string;
  version: number;
  verdict: string;
  author: string;
};
type Reviews = {
  items: Review[];
  mine: Review | null;
  lessons_needed: number;
  completed_lessons: number;
  min_lessons: number;
  max_chars?: number;
  owner: boolean;
  can_review: boolean;
  rating_count: number;
  rating_average: number | null;
};

/** The same inline editor and review list work on desktop and phone. Text is never HTML. */
export function CourseReviews({
  courseId,
  refreshKey,
  onRatingChange,
}: {
  courseId: string;
  refreshKey?: string;
  onRatingChange?: (count: number, average: number | null) => void;
}) {
  const { t, number } = useUiPreferences();
  const [data, setData] = useState<Reviews>();
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [stars, setStars] = useState(0);
  const [text, setText] = useState("");
  const [status, setStatus] = useState("");
  const [confirm, setConfirm] = useState(false);
  const [reporting, setReporting] = useState<string>();
  const heading = useRef<HTMLHeadingElement>(null);
  const dialog = useRef<HTMLDialogElement>(null);
  const deleteButton = useRef<HTMLButtonElement>(null);
  const saving = useRef(false);
  const base = `/v1/marketplace/courses/${encodeURIComponent(courseId)}`;
  const load = useCallback(async () => {
    const next = await session.request<Reviews>(`${base}/reviews`);
    setData(next);
    return next;
  }, [base]);
  useEffect(() => {
    let active = true;
    session.request<Reviews>(`${base}/reviews`).then(
      (next) => {
        if (!active) return;
        setData(next);
        setStars(next.mine?.stars ?? 0);
        setText(next.mine?.text ?? "");
      },
      (e: unknown) => active && setError(marketMessages[marketFailure(e)]),
    );
    return () => {
      active = false;
    };
  }, [base, refreshKey]);
  useEffect(() => {
    const node = dialog.current;
    const returnTarget = deleteButton.current;
    if (!confirm || !node) return;
    if (typeof node.showModal === "function") node.showModal();
    else node.setAttribute("open", "");
    node.querySelector<HTMLButtonElement>("button")?.focus();
    return () => {
      if (typeof node.close === "function") node.close();
      else node.removeAttribute("open");
      returnTarget?.focus();
    };
  }, [confirm]);
  const retry = async () => {
    setError("");
    try {
      const next = await load();
      if (!data) {
        setStars(next.mine?.stars ?? 0);
        setText(next.mine?.text ?? "");
      }
    } catch (e) {
      setError(marketMessages[marketFailure(e)]);
    }
  };
  /** Resolves false when nothing was saved, so a chat can keep its answers. */
  const run = async (
    remove = false,
    values?: { stars: number; text: string },
  ): Promise<boolean> => {
    if (saving.current) return false;
    saving.current = true;
    setBusy(true);
    setError("");
    setStatus("");
    try {
      if (remove) {
        await session.request(`${base}/review`, "DELETE");
        setText("");
        setStars(0);
        setConfirm(false);
        setStatus(t("review.deleted"));
      } else {
        const r = await session.request<Review>(`${base}/review`, "PUT", {
          stars: values?.stars ?? stars,
          text: values?.text ?? text,
        });
        setText(r.text ?? "");
        setStatus(
          r.verdict === "pass" ? t("review.saved") : t("review.hidden"),
        );
      }
      const next = await load();
      onRatingChange?.(next.rating_count, next.rating_average);
      if (remove) heading.current?.focus();
      return true;
    } catch (e) {
      setError(marketMessages[marketFailure(e)]);
      return false;
    } finally {
      saving.current = false;
      setBusy(false);
    }
  };
  const chars = Array.from(text).length;
  const maxChars = data?.max_chars ?? 2000;
  const guided = guidedEnabled("rest");
  return (
    <section
      className="course-reviews"
      data-testid="course-reviews"
      aria-busy={busy || !data}
    >
      <h2 ref={heading} tabIndex={-1}>
        {t("review.title")}
      </h2>
      {!data && !error && <p role="status">{t("review.loading")}</p>}
      {error && (
        <div className="sharing-error" role="alert">
          <p>{error}</p>
          <button
            type="button"
            className="duo-button secondary"
            onClick={() => void retry()}
          >
            {t("review.retry")}
          </button>
        </div>
      )}
      {status && (
        <p role="status" data-testid="review-status">
          {status}
        </p>
      )}
      {data && (
        <>
          <p className="market-meta">
            {t("review.count", { count: data.rating_count })}
            {data.rating_average !== null
              ? ` · ★ ${number(data.rating_average)}`
              : ""}
          </p>
          {data.owner ? (
            <p data-testid="review-gate">{t("review.owner")}</p>
          ) : !data.can_review ? (
            <div className="sharing-note" data-testid="review-gate">
              <strong>{t("review.unlock", { count: data.min_lessons })}</strong>
              <p>
                {t("review.progress", {
                  completed: data.completed_lessons,
                  needed: data.lessons_needed,
                })}
              </p>
            </div>
          ) : guided ? (
            <div className="review-editor">
              <h3>
                {data.mine
                  ? t("review.yours_version", { version: data.mine.version })
                  : t("review.yours")}
              </h3>
              <Suspense fallback={null}>
                <GuidedReview
                  key={`${data.mine?.id ?? "new"}:${data.mine?.stars}:${data.mine?.text}`}
                  mine={data.mine}
                  maxChars={maxChars}
                  onSave={(n, x) => run(false, { stars: n, text: x })}
                />
              </Suspense>
              {data.mine && (
                <div className="share-slide-nav">
                  <button
                    ref={deleteButton}
                    type="button"
                    className="duo-button secondary"
                    disabled={busy}
                    onClick={() => setConfirm(true)}
                  >
                    {t("review.delete")}
                  </button>
                </div>
              )}
            </div>
          ) : (
            <form
              className="review-editor"
              onSubmit={(e) => {
                e.preventDefault();
                void run();
              }}
            >
              <h3>
                {data.mine
                  ? t("review.yours_version", { version: data.mine.version })
                  : t("review.yours")}
              </h3>
              <fieldset disabled={busy}>
                <legend>{t("review.rating")}</legend>
                <div className="review-stars">
                  {[1, 2, 3, 4, 5].map((n) => (
                    <label key={n}>
                      <input
                        type="radio"
                        name={`review-stars-${courseId}`}
                        aria-label={t("review.star_label", { count: n })}
                        value={n}
                        checked={stars === n}
                        onChange={() => setStars(n)}
                      />
                      <span aria-hidden="true">★</span>
                      <span>{n}</span>
                    </label>
                  ))}
                </div>
              </fieldset>
              <label htmlFor={`review-text-${courseId}`}>
                {t("review.text")}
              </label>
              <textarea
                id={`review-text-${courseId}`}
                rows={4}
                value={text}
                onChange={(e) => setText(e.target.value)}
                disabled={busy}
                aria-describedby={`review-help-${courseId}`}
                aria-invalid={chars > maxChars}
              />
              <p className="sharing-note" id={`review-help-${courseId}`}>
                {t("review.text_help", { count: chars, max: maxChars })}
              </p>
              <div className="share-slide-nav">
                <button
                  type="submit"
                  className="duo-button"
                  disabled={busy || stars === 0 || chars > maxChars}
                >
                  {busy
                    ? t("review.saving")
                    : data.mine
                      ? t("review.save")
                      : t("review.post")}
                </button>
                {data.mine && (
                  <button
                    ref={deleteButton}
                    type="button"
                    className="duo-button secondary"
                    disabled={busy}
                    onClick={() => setConfirm(true)}
                  >
                    {t("review.delete")}
                  </button>
                )}
              </div>
            </form>
          )}
          {data.items.length === 0 ? (
            <p>{t("review.empty")}</p>
          ) : (
            <ol className="review-list" aria-label={t("review.list")}>
              {data.items.map((r) => (
                <li key={r.id}>
                  <strong dir="auto">
                    {t("review.item", {
                      author: r.author,
                      stars: "★".repeat(r.stars),
                      version: r.version,
                    })}
                  </strong>
                  {r.text ? (
                    <p dir="auto">{r.text}</p>
                  ) : (
                    r.verdict !== "pass" && (
                      <p className="sharing-note">{t("review.hidden_text")}</p>
                    )
                  )}
                  {data.mine?.id !== r.id && (
                    <button
                      type="button"
                      className="report-link"
                      onClick={() => setReporting(r.id)}
                    >
                      Report review
                    </button>
                  )}
                </li>
              ))}
            </ol>
          )}
        </>
      )}
      {reporting && (
        <ReportSheet
          target={{ type: "review", id: reporting }}
          title="Report this review"
          onClose={() => setReporting(undefined)}
        />
      )}
      {confirm && (
        <dialog
          ref={dialog}
          className="review-confirm"
          aria-labelledby="review-delete-title"
          onCancel={(e) => {
            e.preventDefault();
            if (!busy) setConfirm(false);
          }}
          onKeyDown={(e) => {
            if (e.key !== "Tab") return;
            const buttons = Array.from(
              dialog.current?.querySelectorAll<HTMLButtonElement>(
                "button:not(:disabled)",
              ) ?? [],
            );
            if (e.shiftKey && document.activeElement === buttons[0]) {
              e.preventDefault();
              buttons[buttons.length - 1]?.focus();
            } else if (
              !e.shiftKey &&
              document.activeElement === buttons[buttons.length - 1]
            ) {
              e.preventDefault();
              buttons[0]?.focus();
            }
          }}
        >
          <h2 id="review-delete-title">{t("review.confirm_title")}</h2>
          <p>{t("review.confirm_body")}</p>
          {error && <p role="alert">{error}</p>}
          <div className="share-slide-nav">
            <button
              type="button"
              className="duo-button secondary"
              disabled={busy}
              onClick={() => setConfirm(false)}
            >
              {t("review.cancel")}
            </button>
            <button
              type="button"
              className="duo-button"
              disabled={busy}
              onClick={() => void run(true)}
            >
              {t("review.confirm")}
            </button>
          </div>
        </dialog>
      )}
    </section>
  );
}
