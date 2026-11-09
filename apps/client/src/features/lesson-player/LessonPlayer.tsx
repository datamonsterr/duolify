import { useUiPreferences } from "../design/localization/Preferences";
import { useEffect, useMemo, useRef, useState } from "react";
import { Link } from "@tanstack/react-router";
import {
  track,
  trackCharacterRendered,
  trackCharacterRenderFallback,
} from "../../lib/analytics";
import { isSlideMessage, nextSlide, sendCommand } from "./bridge";
import slide1 from "../../slides/one.html?raw";
import slide2 from "../../slides/two.html?raw";
import slide3 from "../../slides/three.html?raw";
import { PanelLayout, ResponsivePanel } from "../shell/AdaptiveLayouts";
import { useShellSession } from "../shell/ShellSession";
import { createDefaultTracerManifest } from "../characters/fixtures";
import { generateMotionSlideHtml } from "../characters/motion-slide";

const defaultSlides = [slide1, slide2, slide3];
const playbackSpeeds = [1, 1.25, 1.5, 2];

export function LessonPlayer({ lessonId = "sky" }: { lessonId?: string } = {}) {
  const { reducedMotion } = useUiPreferences();
  const session = useShellSession();
  const [index, setIndex] = useState(() => session.slides.get(lessonId) ?? 0);
  useEffect(() => {
    session.slides.set(lessonId, index);
  }, [session, lessonId, index]);
  const [finished, setFinished] = useState(false);
  const [isPlaying, setIsPlaying] = useState(true);
  const [isMuted, setIsMuted] = useState(false);
  const [speedIndex, setSpeedIndex] = useState(0);
  const [slideTime, setSlideTime] = useState(0);
  const frame = useRef<HTMLIFrameElement>(null);

  const tracerManifest = useMemo(() => {
    if (lessonId === "tracer-default-cast" || lessonId.startsWith("tracer")) {
      return createDefaultTracerManifest();
    }
    return null;
  }, [lessonId]);

  const slides = useMemo(() => {
    if (tracerManifest) {
      return tracerManifest.slides.map((s) =>
        generateMotionSlideHtml(s, tracerManifest.cast, {
          reducedMotion,
          offline: typeof navigator !== "undefined" ? !navigator.onLine : false,
        }),
      );
    }
    return defaultSlides;
  }, [tracerManifest, reducedMotion]);

  useEffect(() => {
    sendCommand(frame.current?.contentWindow ?? null, {
      type: "set_motion",
      reduced: reducedMotion,
    });
  }, [reducedMotion, index]);
  const [panel, setPanel] = useState<
    "Transcript" | "Ask tutor" | "Quick note" | null
  >(null);
  const [note, setNote] = useState("");
  const pause = () => {
    setIsPlaying(false);
    sendCommand(frame.current?.contentWindow ?? null, { type: "pause" });
  };
  const goToSlide = (nextIndex: number) => {
    setIndex(nextIndex);
    setSlideTime(0);
    setIsPlaying(true);
  };
  useEffect(() => {
    const playerFrame = frame.current;
    const onVisibility = () => {
      if (document.hidden) {
        setIsPlaying(false);
        sendCommand(frame.current?.contentWindow ?? null, { type: "pause" });
      }
    };
    document.addEventListener("visibilitychange", onVisibility);
    return () => {
      document.removeEventListener("visibilitychange", onVisibility);
      sendCommand(playerFrame?.contentWindow ?? null, { type: "pause" });
    };
  }, []);

  // Keyboard navigation parity: Space for play/pause, Left/Right for seek
  const handleKeyAction = (code: string, key: string) => {
    if (code === "Space" || key === " ") {
      setIsPlaying((prev) => {
        const next = !prev;
        sendCommand(frame.current?.contentWindow ?? null, {
          type: next ? "play" : "pause",
        });
        return next;
      });
    } else if (code === "ArrowRight" || key === "ArrowRight") {
      setSlideTime((prev) => {
        const next = Math.min(3000, prev + 500);
        sendCommand(frame.current?.contentWindow ?? null, {
          type: "seek",
          time: next,
        });
        return next;
      });
    } else if (code === "ArrowLeft" || key === "ArrowLeft") {
      setSlideTime((prev) => {
        const next = Math.max(0, prev - 500);
        sendCommand(frame.current?.contentWindow ?? null, {
          type: "seek",
          time: next,
        });
        return next;
      });
    }
  };

  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      const target = e.target as HTMLElement | null;
      if (
        target &&
        (target.tagName === "INPUT" ||
          target.tagName === "TEXTAREA" ||
          target.isContentEditable)
      ) {
        return;
      }
      if (
        e.code === "Space" ||
        e.key === " " ||
        e.code === "ArrowRight" ||
        e.key === "ArrowRight" ||
        e.code === "ArrowLeft" ||
        e.key === "ArrowLeft"
      ) {
        e.preventDefault();
        handleKeyAction(e.code, e.key);
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, []);

  useEffect(() => {
    track("lesson_started", { lesson: lessonId });
  }, [lessonId]);
  useEffect(() => {
    track("slide_viewed", { slide: index + 1 });
  }, [index]);
  useEffect(() => {
    const listener = (event: MessageEvent) => {
      if (!event.data || typeof event.data !== "object") return;

      if (
        isSlideMessage(event, frame.current?.contentWindow ?? null) &&
        event.data.type === "ended"
      ) {
        /* Learner advances when ready. */
      }

      if (event.data.type === "player_keydown") {
        handleKeyAction(event.data.code, event.data.key);
      } else if (event.data.type === "character_rendered") {
        trackCharacterRendered({
          speaker_key: event.data.speaker_key,
          character_version: event.data.character_version,
          rig_version: event.data.rig_version,
          expression: event.data.expression,
          offline: Boolean(event.data.offline),
          slide_id: event.data.slide_id,
        });
      } else if (event.data.type === "character_render_fallback") {
        trackCharacterRenderFallback({
          reason_code: event.data.reason_code,
          offline: Boolean(event.data.offline),
        });
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
                {tracerManifest
                  ? tracerManifest.slides[index]?.segments
                      .map((s) => s.text)
                      .join(" ")
                  : [
                      "The sky is a map waiting to be explored.",
                      "Stars are distant suns, shining across space.",
                      "Notice how the sky changes as Earth turns.",
                    ][index]}
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
          title={`Slide ${index + 1}`}
          sandbox="allow-scripts"
          srcDoc={slides[index]}
          onLoad={() => {
            sendCommand(frame.current?.contentWindow ?? null, {
              type: "set_motion",
              reduced: reducedMotion,
            });
            sendCommand(frame.current?.contentWindow ?? null, {
              type: "rate",
              rate: playbackSpeeds[speedIndex],
            });
            if (!isPlaying)
              sendCommand(frame.current?.contentWindow ?? null, {
                type: "pause",
              });
          }}
        />
        <div className="controls player-controls">
          <button
            onClick={() => goToSlide(Math.max(0, index - 1))}
            disabled={index === 0}
          >
            Previous
          </button>
          <button
            aria-label="Replay slide"
            onClick={() => {
              setSlideTime(0);
              setIsPlaying(true);
              sendCommand(frame.current?.contentWindow ?? null, {
                type: "seek",
                time: 0,
              });
            }}
          >
            Replay
          </button>
          <button
            onClick={() => {
              const nextPlaying = !isPlaying;
              setIsPlaying(nextPlaying);
              sendCommand(frame.current?.contentWindow ?? null, {
                type: nextPlaying ? "play" : "pause",
              });
            }}
          >
            {isPlaying ? "Pause" : "Play"}
          </button>
          <div className="audio-timeline">
            <input
              className="audio-timeline-slider"
              type="range"
              min={0}
              max={3000}
              step={100}
              value={slideTime}
              aria-label="Slide time"
              aria-valuetext={`${Math.floor(slideTime / 1000)}:${String(Math.floor((slideTime % 1000) / 100)).padStart(2, "0")} of 0:03`}
              onChange={(event) => {
                const time = Number(event.currentTarget.value);
                setSlideTime(time);
                setIsPlaying(true);
                sendCommand(frame.current?.contentWindow ?? null, {
                  type: "seek",
                  time,
                });
              }}
            />
            <span aria-hidden="true">
              {Math.floor(slideTime / 1000)}:
              {String(Math.floor((slideTime % 1000) / 100)).padStart(2, "0")} /
              0:03
            </span>
          </div>
          <button
            aria-label={`Speed ${playbackSpeeds[speedIndex]}x`}
            onClick={() => {
              const nextIndex = (speedIndex + 1) % playbackSpeeds.length;
              setSpeedIndex(nextIndex);
              sendCommand(frame.current?.contentWindow ?? null, {
                type: "rate",
                rate: playbackSpeeds[nextIndex],
              });
            }}
          >
            {playbackSpeeds[speedIndex]}×
          </button>
          <button
            aria-label={isMuted ? "Unmute narration" : "Mute narration"}
            aria-pressed={isMuted}
            onClick={() => setIsMuted(!isMuted)}
          >
            {isMuted ? "Unmute" : "Mute"}
          </button>
          <button
            className="primary"
            onClick={() => {
              if (index === slides.length - 1) {
                track("lesson_completed", { lesson: lessonId });
                setFinished(true);
              } else goToSlide(nextSlide(index, slides.length));
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
