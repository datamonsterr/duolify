import { useUiPreferences } from "../design/localization/Preferences";
import { useEffect, useRef, useState } from "react";
import { Link } from "@tanstack/react-router";
import { track } from "../../lib/analytics";
import { isSlideMessage, nextSlide, sendCommand } from "./bridge";
import slide1 from "../../slides/one.html?raw";
import slide2 from "../../slides/two.html?raw";
import slide3 from "../../slides/three.html?raw";
import { PanelLayout, ResponsivePanel } from "../shell/AdaptiveLayouts";
import { useShellSession } from "../shell/ShellSession";
const slides = [slide1, slide2, slide3];
export function LessonPlayer({ lessonId = "sky" }: { lessonId?: string } = {}) {
  const { reducedMotion } = useUiPreferences();
  const session = useShellSession();
  const [index, setIndex] = useState(() => session.slides.get(lessonId) ?? 0);
  useEffect(() => {
    session.slides.set(lessonId, index);
  }, [session, lessonId, index]);
  const [finished, setFinished] = useState(false);
  useEffect(() => {
    sendCommand(frame.current?.contentWindow ?? null, {
      type: "set_motion",
      reduced: reducedMotion,
    });
  }, [reducedMotion, index]);
  const frame = useRef<HTMLIFrameElement>(null);
  const [panel, setPanel] = useState<
    "Transcript" | "Ask tutor" | "Quick note" | null
  >(null);
  const [note, setNote] = useState("");
  const pause = () =>
    sendCommand(frame.current?.contentWindow ?? null, { type: "pause" });
  useEffect(() => {
    const playerFrame = frame.current;
    const onVisibility = () => {
      if (document.hidden)
        sendCommand(frame.current?.contentWindow ?? null, { type: "pause" });
    };
    document.addEventListener("visibilitychange", onVisibility);
    return () => {
      document.removeEventListener("visibilitychange", onVisibility);
      sendCommand(playerFrame?.contentWindow ?? null, { type: "pause" });
    };
  }, []);
  useEffect(() => {
    track("lesson_started", { lesson: "sky" });
  }, []);
  useEffect(() => {
    track("slide_viewed", { slide: index + 1 });
  }, [index]);
  useEffect(() => {
    const listener = (event: MessageEvent) => {
      if (
        isSlideMessage(event, frame.current?.contentWindow ?? null) &&
        event.data.type === "ended"
      ) {
        /* Learner advances when ready. */
      }
    };
    window.addEventListener("message", listener);
    return () => window.removeEventListener("message", listener);
  }, []);
  if (finished)
    return (
      <div className="complete">
        <div className="big-star">✦</div>
        <h1>Lesson complete!</h1>
        <p>Wonderful work. Your universe just got a little bigger.</p>
        <Link to="/course/$courseId" params={{ courseId: session.courseId() }}>
          Back to your path
        </Link>
      </div>
    );
  return (
    <div className="player">
      <header className="focused-context">
        <Link
          className="shell-link"
          to="/course/$courseId"
          params={{ courseId: session.courseId() }}
          onClick={pause}
        >
          Exit lesson
        </Link>
        <span>Course · Lesson</span>
      </header>
      <h1 tabIndex={-1}>Why the sky changes</h1>
      <div
        className="progress"
        role="progressbar"
        aria-label="Lesson slides"
        aria-valuemin={0}
        aria-valuemax={slides.length}
        aria-valuenow={index + 1}
      >
        <span style={{ width: `${((index + 1) / slides.length) * 100}%` }} />
      </div>
      <p>
        Slide {index + 1} of {slides.length}
      </p>
      <PanelLayout
        panel={
          <ResponsivePanel
            title={panel ?? "Transcript"}
            open={panel !== null}
            onClose={() => setPanel(null)}
          >
            <p className="feedback badge-warning" role="status">
              Lesson paused
            </p>
            {panel === "Transcript" && (
              <p>
                {
                  [
                    "The sky is a map waiting to be explored.",
                    "Stars are distant suns, shining across space.",
                    "Notice how the sky changes as Earth turns.",
                  ][index]
                }
              </p>
            )}
            {panel === "Ask tutor" && (
              <>
                <p>
                  Tutor chat is not available yet. You can keep learning or read
                  the transcript.
                </p>
                <button
                  className="duo-button quiet"
                  onClick={() => setPanel("Transcript")}
                >
                  Read transcript
                </button>
              </>
            )}
            <div hidden={panel !== "Quick note"}>
              <label>
                Your note
                <textarea
                  value={note}
                  onChange={(event) => setNote(event.target.value)}
                />
              </label>
              <p>
                This draft stays here until you leave the lesson. Saving isn't
                available yet.
              </p>
              <button className="duo-button quiet" disabled>
                Save quick note unavailable
              </button>
            </div>
          </ResponsivePanel>
        }
      >
        <iframe
          ref={frame}
          onLoad={() =>
            sendCommand(frame.current?.contentWindow ?? null, {
              type: "set_motion",
              reduced: reducedMotion,
            })
          }
          title={`Slide ${index + 1}`}
          sandbox="allow-scripts"
          srcDoc={slides[index]}
        />
        <div className="controls">
          <button
            onClick={() => setIndex(Math.max(0, index - 1))}
            disabled={index === 0}
          >
            Previous
          </button>
          <button
            onClick={() =>
              sendCommand(frame.current?.contentWindow ?? null, {
                type: "seek",
                time: 0,
              })
            }
          >
            Replay
          </button>
          <button
            onClick={() =>
              sendCommand(frame.current?.contentWindow ?? null, {
                type: "pause",
              })
            }
          >
            Pause
          </button>
          <button
            onClick={() =>
              sendCommand(frame.current?.contentWindow ?? null, {
                type: "play",
              })
            }
          >
            Play
          </button>
          <button
            className="primary"
            onClick={() => {
              if (index === slides.length - 1) {
                track("lesson_completed", { lesson: "sky" });
                setFinished(true);
              } else setIndex(nextSlide(index, slides.length));
            }}
          >
            {index === slides.length - 1 ? "Finish lesson" : "Next"}
          </button>
        </div>
        <div className="panel-tools" aria-label="Lesson tools">
          {(["Transcript", "Ask tutor", "Quick note"] as const).map((name) => (
            <button
              className="duo-button quiet"
              key={name}
              aria-expanded={panel === name}
              onClick={() => {
                pause();
                setPanel(name);
              }}
            >
              {name}
            </button>
          ))}
        </div>
      </PanelLayout>
      {index + 1 < slides.length && (
        <iframe
          title="Preloaded slide"
          hidden
          sandbox="allow-scripts"
          srcDoc={slides[index + 1]}
        />
      )}
    </div>
  );
}
