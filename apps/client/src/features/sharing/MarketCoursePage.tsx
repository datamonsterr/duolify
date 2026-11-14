import { useCallback, useEffect, useRef, useState } from "react";
import {
  enrollMarketplace,
  fetchMarketCourse,
  learnersLabel,
  marketFailure,
  marketMessages,
  ratingLabel,
  type MarketCourse,
  type MarketFailure,
} from "./market";
import { CourseReviews } from "./CourseReviews";
import { ReportSheet } from "./ReportSheet";
import type { ReportTarget } from "./reports";
import type { EnrollResult } from "./links";
import "./sharing.css";
import "./market.css";

type State =
  | { kind: "loading" }
  | { kind: "ready"; course: MarketCourse }
  | { kind: "failed"; failure: MarketFailure };
type Enrolled =
  | { kind: "idle" }
  | { kind: "pending" }
  | { kind: "done"; result: EnrollResult }
  | { kind: "failed"; failure: MarketFailure };

const published = (iso: string) =>
  new Date(iso).toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
  });

/** A listed Course: attribution, version, outline, safe preview, ready versus paid, free enroll. */
export function MarketCoursePage({ id }: { id: string }) {
  const [state, setState] = useState<State>({ kind: "loading" });
  const [slide, setSlide] = useState(0);
  const [enroll, setEnroll] = useState<Enrolled>({ kind: "idle" });
  const busy = useRef(false);
  const [menu, setMenu] = useState(false);
  const [report, setReport] = useState<{
    target: ReportTarget;
    title: string;
  }>();
  const more = useRef<HTMLButtonElement>(null);

  const load = useCallback(() => {
    let active = true;
    fetchMarketCourse(id).then(
      (course) => active && setState({ kind: "ready", course }),
      (error: unknown) =>
        active && setState({ kind: "failed", failure: marketFailure(error) }),
    );
    return () => {
      active = false;
    };
  }, [id]);
  useEffect(() => load(), [load]);
  const retry = () => {
    setState({ kind: "loading" });
    load();
  };

  const run = () => {
    if (busy.current) return;
    busy.current = true;
    setEnroll({ kind: "pending" });
    enrollMarketplace(id)
      .then(
        (result) => setEnroll({ kind: "done", result }),
        (error: unknown) =>
          setEnroll({ kind: "failed", failure: marketFailure(error) }),
      )
      .finally(() => {
        busy.current = false;
      });
  };

  if (state.kind === "loading")
    return (
      <div className="market" aria-busy="true">
        <p role="status">
          <span className="sharing-spinner" aria-hidden="true" />
          Loading course…
        </p>
      </div>
    );
  if (state.kind === "failed" && state.failure === "unavailable")
    return (
      <div className="market">
        <h1>This course isn't available</h1>
        <p>It may have been removed or is no longer listed.</p>
        <a className="duo-button secondary" href="/marketplace">
          Back to Marketplace
        </a>
      </div>
    );
  if (state.kind === "failed")
    return (
      <div className="market">
        <div className="sharing-error" role="alert">
          <p>{marketMessages[state.failure]}</p>
          <button type="button" className="duo-button" onClick={retry}>
            Retry
          </button>
        </div>
      </div>
    );

  const c = state.course;
  const slides = c.sample_slides;
  const name = c.creator.username
    ? `@${c.creator.username}`
    : c.creator.display_name || "a Duolify creator";
  const copy = c.viewer.course_id;
  return (
    <div className="market market-course">
      <a className="market-back" href="/marketplace">
        ← Marketplace
      </a>
      <div className="market-title-row">
        <h1 dir="auto">{c.title}</h1>
        {!c.viewer.owner && (
          <div
            className="report-menu"
            onKeyDown={(e) => {
              if (e.key === "Escape" && menu) {
                e.stopPropagation();
                setMenu(false);
                more.current?.focus();
              }
            }}
          >
            <button
              ref={more}
              type="button"
              className="report-more"
              aria-label="More: report course"
              aria-haspopup="menu"
              aria-expanded={menu}
              onClick={() => setMenu(!menu)}
            >
              <span aria-hidden="true">⋯</span>
            </button>
            {menu && (
              <div role="menu" className="report-menu-list">
                <button
                  type="button"
                  role="menuitem"
                  autoFocus
                  onClick={() => {
                    setMenu(false);
                    setReport({
                      target: { type: "course", id },
                      title: "Report this course",
                    });
                  }}
                >
                  Report this course
                </button>
                {c.creator.username && (
                  <button
                    type="button"
                    role="menuitem"
                    onClick={() => {
                      setMenu(false);
                      setReport({
                        target: { type: "user", id: c.creator.username },
                        title: "Report this creator",
                      });
                    }}
                  >
                    Report creator
                  </button>
                )}
              </div>
            )}
          </div>
        )}
      </div>
      <p className="market-meta" data-testid="creator">
        by {name} · v{c.version} · published {published(c.published_at)} ·{" "}
        {ratingLabel(c)} · {learnersLabel(c.enrollment_count)}
      </p>
      <p dir="auto">{c.description}</p>
      {slides.length > 0 ? (
        <section aria-label="Sample slides" className="share-slides">
          <iframe
            key={slides[slide]!.id}
            title={`Sample slide ${slide + 1}`}
            sandbox=""
            srcDoc={slides[slide]!.html}
            className="share-slide-frame"
          />
          <div className="share-slide-nav">
            <button
              type="button"
              className="duo-button quiet"
              disabled={slide === 0}
              onClick={() => setSlide(slide - 1)}
            >
              Previous slide
            </button>
            <span aria-live="polite">
              Preview {slide + 1} of {slides.length}
            </span>
            <button
              type="button"
              className="duo-button quiet"
              disabled={slide >= slides.length - 1}
              onClick={() => setSlide(slide + 1)}
            >
              Next slide
            </button>
          </div>
        </section>
      ) : (
        <p className="sharing-note">No sample slides yet.</p>
      )}
      <p data-testid="lessons-ready" className="market-ready">
        {c.lessons_ready} of {c.lessons_total} lessons ready
      </p>
      <p data-testid="paid-note" className="sharing-note">
        Ready lessons are free for you. The rest are generated as you learn and
        cost Credits.
      </p>

      <section className="enroll-panel" aria-label="Enroll">
        {c.viewer.owner && <p>This is your course.</p>}
        {!c.viewer.owner && c.viewer.enrolled && enroll.kind !== "done" && (
          <div className="enroll-done">
            <p data-testid="enroll-status">Already enrolled</p>
            <a className="duo-button" href={`/course/${copy}`}>
              Open course
            </a>
          </div>
        )}
        {enroll.kind === "done" && (
          <div className="enroll-done">
            <p data-testid="enroll-status">
              {enroll.result.created ? "Enrolled" : "Already enrolled"}
            </p>
            <p className="sharing-note">
              Version {enroll.result.version} · {enroll.result.ready_lessons} of{" "}
              {enroll.result.total_lessons} lessons ready
            </p>
            <a
              className="duo-button"
              href={`/course/${enroll.result.course_id}`}
            >
              Open course
            </a>
          </div>
        )}
        {!c.viewer.owner && !c.viewer.enrolled && enroll.kind !== "done" && (
          <>
            {enroll.kind === "failed" && (
              <div className="sharing-error" role="alert">
                <p>{marketMessages[enroll.failure]}</p>
              </div>
            )}
            <button
              type="button"
              className="duo-button"
              disabled={enroll.kind === "pending"}
              onClick={run}
            >
              Enroll · free to start
            </button>
          </>
        )}
      </section>

      <div className="market-course-details">
        <section className="market-outline">
          <h2>Outline</h2>
          <ol aria-label="Course outline" className="share-outline">
            {c.outline.map((unit, i) => (
              <li key={i}>
                <strong dir="auto">{unit.title}</strong>
                <ul>
                  {unit.lessons.map((l, j) => (
                    <li key={j} dir="auto">
                      {l.title}
                      {l.generated ? "" : " · generated as you learn"}
                    </li>
                  ))}
                </ul>
              </li>
            ))}
          </ol>
        </section>
        <CourseReviews
          courseId={id}
          refreshKey={
            enroll.kind === "done" ? enroll.result.course_id : undefined
          }
          onRatingChange={(count, average) =>
            setState((current) =>
              current.kind === "ready"
                ? {
                    kind: "ready",
                    course: {
                      ...current.course,
                      rating_count: count,
                      rating_average: average,
                    },
                  }
                : current,
            )
          }
        />
      </div>
      {report && (
        <ReportSheet
          target={report.target}
          title={report.title}
          returnFocus={more}
          onClose={() => setReport(undefined)}
        />
      )}
    </div>
  );
}
