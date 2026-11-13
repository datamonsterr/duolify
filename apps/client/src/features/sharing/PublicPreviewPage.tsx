import { useCallback, useEffect, useState } from "react";
import {
  fetchPreview,
  linkFailure,
  linkMessages,
  type LinkFailure,
  type PublicPreview,
} from "./links";
import "./sharing.css";

type State =
  | { kind: "loading" }
  | { kind: "ready"; preview: PublicPreview }
  | { kind: "failed"; failure: LinkFailure };

const appScheme = import.meta.env.VITE_APP_SCHEME || "duolify";

/** Anonymous course link preview. Safe sample slides only; no sign-in needed. */
export function PublicPreviewPage({ token }: { token: string }) {
  const [state, setState] = useState<State>({ kind: "loading" });
  const [slide, setSlide] = useState(0);
  const [outline, setOutline] = useState(false);

  // Public pages must never be indexed, whatever the host serves.
  useEffect(() => {
    const meta = document.createElement("meta");
    meta.name = "robots";
    meta.content = "noindex, nofollow";
    document.head.appendChild(meta);
    return () => meta.remove();
  }, []);

  const load = useCallback(() => {
    let active = true;
    fetchPreview(token)
      .then((preview) => active && setState({ kind: "ready", preview }))
      .catch(
        (error: unknown) =>
          active && setState({ kind: "failed", failure: linkFailure(error) }),
      );
    return () => {
      active = false;
    };
  }, [token]);
  useEffect(() => load(), [load]);
  const retry = () => {
    setState({ kind: "loading" });
    load();
  };

  return (
    <main className="share-preview" aria-busy={state.kind === "loading"}>
      <header className="share-preview-brand">duolify</header>
      {state.kind === "loading" && (
        <p role="status">
          <span className="sharing-spinner" aria-hidden="true" />
          Loading preview…
        </p>
      )}
      {state.kind === "failed" && state.failure === "unavailable" && (
        <section className="share-preview-gone">
          <h1>Link not active</h1>
          <p>
            This course link is no longer active. Ask the creator for a new one.
          </p>
        </section>
      )}
      {state.kind === "failed" && state.failure !== "unavailable" && (
        <div className="sharing-error" role="alert">
          <p>{linkMessages[state.failure]}</p>
          <button type="button" className="duo-button" onClick={retry}>
            Retry
          </button>
        </div>
      )}
      {state.kind === "ready" &&
        (() => {
          const p = state.preview;
          const slides = p.sample_slides;
          const name = p.creator.username
            ? `@${p.creator.username}`
            : p.creator.display_name || "a Duolify creator";
          return (
            <>
              <h1 dir="auto">{p.title}</h1>
              <p className="sharing-meta" data-testid="creator">
                by {name} · version {p.version} ·{" "}
                <span data-testid="enrollment-count">
                  {p.enrollment_count.toLocaleString()} learners
                </span>
              </p>
              <p dir="auto">{p.description}</p>
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
              <p data-testid="lessons-ready">
                {p.generated_lesson_count} of {p.lesson_count} lessons ready
              </p>
              <p className="sharing-note">
                Ready lessons are free. The rest are generated as you learn and
                cost credits.
              </p>
              <div className="sharing-actions">
                <a
                  className="duo-button"
                  href={`/sign-in?returnTo=${encodeURIComponent(`/c/${token}`)}`}
                >
                  Sign in to enroll
                </a>
                <a
                  className="duo-button secondary"
                  href={`${appScheme}://share/${encodeURIComponent(token)}`}
                >
                  Open in Duolify app
                </a>
                <button
                  type="button"
                  className="duo-button quiet"
                  aria-expanded={outline}
                  onClick={() => setOutline(!outline)}
                >
                  See full outline
                </button>
              </div>
              {outline && (
                <ol aria-label="Course outline" className="share-outline">
                  {p.outline.map((unit, i) => (
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
              )}
            </>
          );
        })()}
    </main>
  );
}
