import { useCallback, useEffect, useRef, useState } from "react";
import { session } from "../accounts/session";
import { marketFailure, marketMessages } from "./market";

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
const messages = {
  "review.saved": "Review saved.",
  "review.hidden":
    "Your stars are saved. Your review text is hidden while it awaits approval or needs changes.",
  "review.deleted": "Review deleted.",
  "review.loading": "Loading reviews…",
  "review.empty": "No reviews yet. Share your view after learning.",
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
  const [data, setData] = useState<Reviews>();
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [stars, setStars] = useState(0);
  const [text, setText] = useState("");
  const [status, setStatus] = useState("");
  const [confirm, setConfirm] = useState(false);
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
  const run = async (remove = false) => {
    if (saving.current) return;
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
        setStatus(messages["review.deleted"]);
      } else {
        const r = await session.request<Review>(`${base}/review`, "PUT", {
          stars,
          text,
        });
        setText(r.text ?? "");
        setStatus(
          r.verdict === "pass"
            ? messages["review.saved"]
            : messages["review.hidden"],
        );
      }
      const next = await load();
      onRatingChange?.(next.rating_count, next.rating_average);
      if (remove) heading.current?.focus();
    } catch (e) {
      setError(marketMessages[marketFailure(e)]);
    } finally {
      saving.current = false;
      setBusy(false);
    }
  };
  const chars = Array.from(text).length;
  const maxChars = data?.max_chars ?? 2000;
  return (
    <section
      className="course-reviews"
      data-testid="course-reviews"
      aria-busy={busy || !data}
    >
      <h2 ref={heading} tabIndex={-1}>
        Reviews
      </h2>
      {!data && !error && <p role="status">{messages["review.loading"]}</p>}
      {error && (
        <div className="sharing-error" role="alert">
          <p>{error}</p>
          <button
            type="button"
            className="duo-button secondary"
            onClick={() => void retry()}
          >
            Retry
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
            {data.rating_count.toLocaleString()} reviews
            {data.rating_average !== null
              ? ` · ★ ${data.rating_average.toFixed(1)}`
              : ""}
          </p>
          {data.owner ? (
            <p data-testid="review-gate">You can't review your own course.</p>
          ) : !data.can_review ? (
            <div className="sharing-note" data-testid="review-gate">
              <strong>Reviews open after {data.min_lessons} lessons</strong>
              <p>
                You've finished {data.completed_lessons}. Complete{" "}
                {data.lessons_needed} more to share your view.
              </p>
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
                Your review{data.mine ? ` · on v${data.mine.version}` : ""}
              </h3>
              <fieldset disabled={busy}>
                <legend>Star rating</legend>
                <div className="review-stars">
                  {[1, 2, 3, 4, 5].map((n) => (
                    <label key={n}>
                      <input
                        type="radio"
                        name={`review-stars-${courseId}`}
                        aria-label={`${n} ${n === 1 ? "star" : "stars"}`}
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
              <label htmlFor={`review-text-${courseId}`}>Review text</label>
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
                Optional · {chars.toLocaleString()} /{" "}
                {maxChars.toLocaleString()} characters. Text is checked before
                it appears.
              </p>
              <div className="share-slide-nav">
                <button
                  type="submit"
                  className="duo-button"
                  disabled={busy || stars === 0 || chars > maxChars}
                >
                  {busy ? "Saving…" : data.mine ? "Save review" : "Post review"}
                </button>
                {data.mine && (
                  <button
                    ref={deleteButton}
                    type="button"
                    className="duo-button secondary"
                    disabled={busy}
                    onClick={() => setConfirm(true)}
                  >
                    Delete review
                  </button>
                )}
              </div>
            </form>
          )}
          {data.items.length === 0 ? (
            <p>{messages["review.empty"]}</p>
          ) : (
            <ol className="review-list" aria-label="Course reviews">
              {data.items.map((r) => (
                <li key={r.id}>
                  <strong dir="auto">
                    {r.author} · {"★".repeat(r.stars)} · on v{r.version}
                  </strong>
                  {r.text ? (
                    <p dir="auto">{r.text}</p>
                  ) : (
                    r.verdict !== "pass" && (
                      <p className="sharing-note">Review text is hidden.</p>
                    )
                  )}
                </li>
              ))}
            </ol>
          )}
        </>
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
          <h2 id="review-delete-title">Delete your review?</h2>
          <p>This removes your stars and review from this course.</p>
          {error && <p role="alert">{error}</p>}
          <div className="share-slide-nav">
            <button
              type="button"
              className="duo-button secondary"
              disabled={busy}
              onClick={() => setConfirm(false)}
            >
              Cancel
            </button>
            <button
              type="button"
              className="duo-button"
              disabled={busy}
              onClick={() => void run(true)}
            >
              Confirm delete
            </button>
          </div>
        </dialog>
      )}
    </section>
  );
}
