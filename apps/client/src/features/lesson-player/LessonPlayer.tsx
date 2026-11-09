import { useUiPreferences } from "../design/localization/Preferences";
import { useEffect, useRef, useState } from "react";
import { Link } from "@tanstack/react-router";
import { trackPlayer } from "../../lib/analytics";
import { useQuery } from "@tanstack/react-query";
import { session as accountSession } from "../accounts/session";
import {
  loadManifest,
  loadDocument,
  sandboxDocument,
  type Manifest,
} from "../player/manifest";
import { isSlideMessage, nextSlide, sendCommand } from "./bridge";
import { PanelLayout, ResponsivePanel } from "../shell/AdaptiveLayouts";
import { useShellSession } from "../shell/ShellSession";
const playbackSpeeds = [1, 1.25, 1.5, 2];
export function LessonPlayer({ lessonId = "sky" }: { lessonId?: string } = {}) {
  const owner = accountSession.current()?.id ?? "current";
  const manifest = useQuery({
    queryKey: ["lesson-manifest", owner, lessonId],
    queryFn: () => loadManifest(lessonId),
    retry: false,
    networkMode: "always",
    staleTime: 0,
  });
  useEffect(() => {
    if (manifest.error)
      trackPlayer("lesson_player_error", {
        lesson_id: lessonId,
        error_code:
          manifest.error.message === "invalid_manifest"
            ? "invalid_manifest"
            : "manifest_unavailable",
      });
  }, [manifest.error, lessonId]);
  if (manifest.error)
    return <PlayerRecovery retry={() => void manifest.refetch()} />;
  if (!manifest.data)
    return <PlayerRecovery loading retry={() => void manifest.refetch()} />;
  return <ManifestPlayback key={lessonId} manifest={manifest.data} />;
}
function PlayerRecovery({
  loading = false,
  retry,
}: {
  loading?: boolean;
  retry: () => void;
}) {
  const shell = useShellSession();
  return (
    <section className="player player-recovery" aria-busy={loading}>
      <Link
        className="shell-link"
        to="/course/$courseId"
        params={{ courseId: shell.courseId() }}
      >
        Exit lesson
      </Link>
      <h1>{loading ? "Loading lesson…" : "Lesson unavailable"}</h1>
      <p role={loading ? "status" : "alert"}>
        {loading
          ? "Getting your slides ready."
          : "Lesson unavailable. Check your connection and enrollment, then retry."}
      </p>
      {!loading && (
        <button className="duo-button primary" onClick={retry}>
          Retry
        </button>
      )}
    </section>
  );
}
function ManifestPlayback({ manifest }: { manifest: Manifest }) {
  const lessonId = manifest.lesson_id;
  const slides = manifest.slides;
  const { reducedMotion } = useUiPreferences();
  const session = useShellSession();
  const [index, setIndex] = useState(() =>
    Math.min(session.slides.get(lessonId) ?? 0, slides.length - 1),
  );
  const slide = slides[index];
  const owner = accountSession.current()?.id ?? "current";
  const slideDocument = useQuery({
    queryKey: [
      "lesson-document",
      owner,
      lessonId,
      manifest.version,
      slide.slide_id,
    ],
    queryFn: () => loadDocument(slide),
    retry: false,
    networkMode: "always",
  });
  const next = slides[index + 1];
  const preloaded = useQuery({
    queryKey: [
      "lesson-document",
      owner,
      lessonId,
      manifest.version,
      next?.slide_id,
    ],
    queryFn: () => loadDocument(next),
    enabled: !!next,
    retry: false,
    networkMode: "always",
  });
  const started = useRef(false);
  useEffect(() => {
    if (slideDocument.error)
      trackPlayer("lesson_player_error", {
        lesson_id: lessonId,
        slide_id: slide.slide_id,
        error_code: "document_unavailable",
      });
  }, [slideDocument.error, lessonId, slide.slide_id]);
  useEffect(() => {
    session.slides.set(lessonId, index);
  }, [session, lessonId, index]);
  const [isPlaying, setIsPlaying] = useState(true);
  const [isMuted, setIsMuted] = useState(false);
  const [speedIndex, setSpeedIndex] = useState(0);
  const [slideTime, setSlideTime] = useState(0);
  const frame = useRef<HTMLIFrameElement>(null);
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
  useEffect(() => {
    const listener = (event: MessageEvent) => {
      if (
        isSlideMessage(
          event,
          frame.current?.contentWindow ?? null,
          slide.checkpoints.map((c) => c.id),
        ) &&
        event.data.type === "ended"
      ) {
        /* Learner advances when ready. */
      }
    };
    window.addEventListener("message", listener);
    return () => window.removeEventListener("message", listener);
  }, [slide.checkpoints]);
  return (
    <div className="player">
      <header className="focused-context">
        <Link
          className="shell-link"
          to="/course/$courseId"
          params={{ courseId: manifest.course_id }}
          onClick={pause}
        >
          Exit lesson
        </Link>
        <span>Course · Lesson</span>
      </header>
      <h1 tabIndex={-1}>{manifest.title}</h1>
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
            {panel === "Transcript" && <p>{slide.narration_text}</p>}
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
        {slideDocument.isPending && <p role="status">Loading slide…</p>}
        {slideDocument.error && (
          <div role="alert">
            <p>This slide is unavailable. Check your connection and retry.</p>
            <button
              className="duo-button quiet"
              onClick={() => void slideDocument.refetch()}
            >
              Retry slide
            </button>
          </div>
        )}
        {slideDocument.data && (
          <iframe
            key={slide.slide_id}
            ref={frame}
            title={`Slide ${index + 1}`}
            sandbox="allow-scripts"
            srcDoc={sandboxDocument(
              slideDocument.data,
              manifest.asset_origin,
              reducedMotion,
            )}
            referrerPolicy="no-referrer"
            onLoad={() => {
              if (!started.current) {
                started.current = true;
                trackPlayer("lesson_started", {
                  lesson_id: lessonId,
                  course_id: manifest.course_id,
                });
              }
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
        )}
        <p className="slide-caption">{slide.narration_text}</p>
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
              max={slide.duration_ms}
              step={100}
              value={slideTime}
              aria-label="Slide time"
              aria-valuetext={`${Math.floor(slideTime / 1000)}:${String(Math.floor((slideTime % 1000) / 100)).padStart(2, "0")} of ${Math.floor(slide.duration_ms / 1000)} seconds`}
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
              {Math.floor(slide.duration_ms / 1000)}s
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
          {index === slides.length - 1 ? (
            <Link
              className="duo-button primary"
              to="/lesson/$lessonId/test"
              params={{ lessonId }}
              onClick={pause}
            >
              Take lesson test
            </Link>
          ) : (
            <button
              className="primary"
              onClick={() => goToSlide(nextSlide(index, slides.length))}
            >
              Next
            </button>
          )}
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
      {preloaded.data && (
        <iframe
          title="Preloaded slide"
          hidden
          sandbox="allow-scripts"
          srcDoc={sandboxDocument(
            preloaded.data,
            manifest.asset_origin,
            reducedMotion,
          )}
          referrerPolicy="no-referrer"
        />
      )}
    </div>
  );
}
