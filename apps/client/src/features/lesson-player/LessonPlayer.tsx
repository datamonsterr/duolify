import { useUiPreferences } from "../design/localization/Preferences";
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  useSyncExternalStore,
} from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import {
  track,
  trackPlayer,
  trackPlayerRecovery,
  trackBookmarkAdded,
  trackBookmarkRemoved,
  trackCharacterRendered,
  trackCharacterRenderFallback,
  trackGuidedUi,
} from "../../lib/analytics";
import { availableOffline, preparedDocument } from "../player/cache";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { session } from "../accounts/session";
import {
  loadManifest,
  loadDocument,
  sandboxDocument,
  type Manifest,
} from "../player/manifest";
import {
  enqueueSyncEvent,
  generateActionKey,
  hasPendingSync,
  subscribeSyncStatus,
  synchronizeProgress,
} from "../player/sync";
import {
  loadPosition,
  readLocalPosition,
  writeLocalPosition,
  savePosition,
  playerReturnPath,
  type Position,
} from "../player/position";
import { PlaybackClock } from "../player/playback";
import { isSlideMessage, nextSlide, sendCommand } from "./bridge";
import { PanelLayout, ResponsivePanel } from "../shell/AdaptiveLayouts";
import { useShellSession } from "../shell/ShellSession";
import { QuickNotePanel } from "../notes/QuickNotePanel";
import { TutorChat } from "../chat/TutorChat";
import { bookmarkQueue } from "../notes/bookmark-queue";
import { createDefaultTracerManifest } from "../characters/fixtures";
import { generateMotionSlideHtml } from "../characters/motion-slide";
import { generatedCharacterDocument } from "../characters/playback";

const speeds = [1, 1.25, 1.5, 2, 0.75];

export function LessonPlayer({
  lessonId = "sky",
  returnTo,
  targetSlideId,
  initialSlide,
}: {
  lessonId?: string;
  returnTo?: string;
  targetSlideId?: string;
  initialSlide?: number;
} = {}) {
  if (lessonId === "tracer-default-cast" || lessonId.startsWith("tracer")) {
    return <TracerLessonPlayer lessonId={lessonId} returnTo={returnTo} />;
  }
  return (
    <StandardLessonPlayer
      lessonId={lessonId}
      returnTo={returnTo}
      targetSlideId={targetSlideId}
      initialSlide={initialSlide}
    />
  );
}

function StandardLessonPlayer({
  lessonId,
  returnTo,
  targetSlideId,
  initialSlide,
}: {
  lessonId: string;
  returnTo?: string;
  targetSlideId?: string;
  initialSlide?: number;
}) {
  const owner = session.current()?.id ?? "current";
  const shell = useShellSession();
  const exit = playerReturnPath(returnTo, shell.courseId());
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
          manifest.error.message === "offline_uncached"
            ? "offline_uncached"
            : manifest.error.message === "invalid_manifest"
              ? "invalid_manifest"
              : "manifest_unavailable",
      });
  }, [manifest.error, lessonId]);
  if (manifest.error)
    return (
      <PlayerRecovery
        offline={manifest.error.message === "offline_uncached"}
        exit={exit}
        retry={() => void manifest.refetch()}
      />
    );
  if (!manifest.data)
    return (
      <PlayerRecovery
        exit={exit}
        loading
        retry={() => void manifest.refetch()}
      />
    );
  return (
    <ResumePlayback
      key={`${owner}:${lessonId}:${manifest.data.version}`}
      manifest={manifest.data}
      returnTo={returnTo}
      targetSlideId={targetSlideId}
      initialSlide={initialSlide}
    />
  );
}

function TracerLessonPlayer({
  lessonId = "tracer-default-cast",
  returnTo,
}: {
  lessonId?: string;
  returnTo?: string;
}) {
  const { reducedMotion } = useUiPreferences();
  const sessionShell = useShellSession();
  const [index, setIndex] = useState(
    () => sessionShell.slides.get(lessonId) ?? 0,
  );
  useEffect(() => {
    sessionShell.slides.set(lessonId, index);
  }, [sessionShell, lessonId, index]);
  const [finished, setFinished] = useState(false);
  const [isPlaying, setIsPlaying] = useState(true);
  const [isMuted, setIsMuted] = useState(false);
  const [speedIndex, setSpeedIndex] = useState(0);
  const [slideTime, setSlideTime] = useState(0);
  const frame = useRef<HTMLIFrameElement>(null);

  const tracerManifest = useMemo(() => {
    return createDefaultTracerManifest();
  }, []);

  const slides = useMemo(() => {
    return tracerManifest.slides.map((s) =>
      generateMotionSlideHtml(s, tracerManifest.cast, {
        reducedMotion,
        offline: typeof navigator !== "undefined" ? !navigator.onLine : false,
      }),
    );
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
      if (!isSlideMessage(event, frame.current?.contentWindow ?? null)) return;
      if (event.data.type === "ended") {
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
  }, [lessonId]);

  const exitPath = playerReturnPath(returnTo, sessionShell.courseId());

  if (finished)
    return (
      <div className="complete">
        <div className="big-star">✦</div>
        <h1>Lesson complete!</h1>
        <p>Wonderful work. Your universe just got a little bigger.</p>
        <Link to={exitPath}>Back to your path</Link>
      </div>
    );

  return (
    <div className="player">
      <header className="focused-context">
        <Link className="shell-link" to={exitPath} onClick={pause}>
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
                {tracerManifest.slides[index]?.segments
                  .map((s) => s.text)
                  .join(" ")}
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
              rate: speeds[speedIndex],
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
            aria-label={`Speed ${speeds[speedIndex]}x`}
            onClick={() => {
              const nextIndex = (speedIndex + 1) % speeds.length;
              setSpeedIndex(nextIndex);
              sendCommand(frame.current?.contentWindow ?? null, {
                type: "rate",
                rate: speeds[nextIndex],
              });
            }}
          >
            {speeds[speedIndex]}×
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

function PlayerRecovery({
  loading = false,
  offline = false,
  retry,
  exit,
}: {
  loading?: boolean;
  offline?: boolean;
  retry: () => void;
  exit: string;
}) {
  return (
    <section className="player player-recovery" aria-busy={loading}>
      <Link
        className="shell-link"
        to={exit}
        onClick={() => {
          if (offline) trackPlayerRecovery("exit");
        }}
      >
        Exit lesson
      </Link>
      <h1>
        {loading
          ? "Loading lesson…"
          : offline
            ? "This lesson isn't downloaded"
            : "Lesson unavailable"}
      </h1>
      <p role={loading ? "status" : "alert"}>
        {loading
          ? "Getting your slides ready."
          : offline
            ? "Return to saved lessons, or download this lesson when you're online."
            : "Lesson unavailable. Check your connection and enrollment, then retry."}
      </p>
      {!loading && (
        <button
          className="duo-button primary"
          onClick={() => {
            if (offline) trackPlayerRecovery("retry");
            retry();
          }}
        >
          Retry
        </button>
      )}
    </section>
  );
}

function ResumePlayback({
  manifest,
  returnTo,
  targetSlideId,
  initialSlide,
}: {
  manifest: Manifest;
  returnTo?: string;
  targetSlideId?: string;
  initialSlide?: number;
}) {
  const owner = session.current()?.id ?? "current";
  const position = useQuery({
    queryKey: ["lesson-position", owner, manifest.lesson_id, manifest.version],
    queryFn: () => loadPosition(manifest),
    retry: false,
    networkMode: "always",
    staleTime: 0,
  });
  if (position.isPending)
    return (
      <PlayerRecovery
        loading
        retry={() => void position.refetch()}
        exit={playerReturnPath(returnTo, manifest.course_id)}
      />
    );
  return (
    <ManifestPlayback
      manifest={manifest}
      returnTo={returnTo}
      initial={readLocalPosition(owner, manifest) ?? position.data ?? null}
      restoreError={!!position.error}
      targetSlideId={targetSlideId}
      initialSlide={initialSlide}
    />
  );
}

function ManifestPlayback({
  manifest,
  returnTo,
  initial,
  restoreError,
  targetSlideId,
  initialSlide,
}: {
  manifest: Manifest;
  returnTo?: string;
  initial: Position | null;
  restoreError: boolean;
  targetSlideId?: string;
  initialSlide?: number;
}) {
  const [index, setIndex] = useState(() => {
    if (
      initialSlide !== undefined &&
      !isNaN(initialSlide) &&
      initialSlide >= 0 &&
      initialSlide < manifest.slides.length
    ) {
      return initialSlide;
    }
    if (targetSlideId) {
      const targetIdx = manifest.slides.findIndex(
        (s) => s.slide_id === targetSlideId,
      );
      if (targetIdx !== -1) return targetIdx;
      const parsed = parseInt(targetSlideId, 10);
      if (
        !isNaN(parsed) &&
        parsed >= 0 &&
        parsed < manifest.slides.length &&
        String(parsed) === targetSlideId
      ) {
        return parsed;
      }
      return 0;
    }
    return Math.max(
      0,
      manifest.slides.findIndex((s) => s.slide_id === initial?.slide_id),
    );
  });
  const [rate, setRate] = useState(1);
  const [muted, setMuted] = useState(false);
  const [resuming, setResuming] = useState(!!initial);
  const started = useRef(false);
  return (
    <SlidePlayback
      key={manifest.slides[index].slide_id}
      manifest={manifest}
      index={index}
      initialTime={resuming ? (initial?.time_ms ?? 0) : 0}
      autoplay={!resuming}
      rate={rate}
      onRate={setRate}
      muted={muted}
      onMute={setMuted}
      restoreError={restoreError}
      exit={playerReturnPath(returnTo, manifest.course_id)}
      onNavigate={(next) => {
        setResuming(false);
        setIndex(next);
      }}
      onStart={() => {
        if (!started.current) {
          started.current = true;
          trackPlayer("lesson_started", {
            lesson_id: manifest.lesson_id,
            course_id: manifest.course_id,
          });
        }
      }}
    />
  );
}

function SlidePlayback({
  manifest,
  index,
  initialTime,
  autoplay,
  rate,
  onRate,
  onNavigate,
  onStart,
  restoreError,
  exit,
  muted,
  onMute,
}: {
  manifest: Manifest;
  index: number;
  initialTime: number;
  autoplay: boolean;
  rate: number;
  onRate: (rate: number) => void;
  onNavigate: (index: number) => void;
  onStart: () => void;
  restoreError: boolean;
  exit: string;
  muted: boolean;
  onMute: (muted: boolean) => void;
}) {
  const lessonId = manifest.lesson_id,
    slide = manifest.slides[index];
  const navigate = useNavigate();
  const owner = session.current()?.id ?? "current";
  const shell = useShellSession();
  const { reducedMotion } = useUiPreferences();
  const frame = useRef<HTMLIFrameElement>(null);
  const [clock] = useState(() => {
    const c = new PlaybackClock(slide.duration_ms, initialTime);
    c.rate(rate, performance.now());
    return c;
  });
  const playback = useSyncExternalStore(clock.subscribe, clock.snapshot);
  const [ready, setReady] = useState(false);
  const [unsaved, setUnsaved] = useState(restoreError);
  const [pendingSync, setPendingSync] = useState(() =>
    hasPendingSync(owner, lessonId),
  );
  const [checkpoint, setCheckpoint] = useState(false);
  const [captionsVisible, setCaptionsVisible] = useState(true);
  const [assetError, setAssetError] = useState(false);
  const [panel, setPanel] = useState<
    "Transcript" | "Ask tutor" | "Quick note" | null
  >(null);
  const viewed = useRef(initialTime === slide.duration_ms);
  const reached = useRef(new Set<string>());
  const saveRevision = useRef(0);

  const [toastMessage, setToastMessage] = useState<string | null>(null);
  const [toastVisible, setToastVisible] = useState(false);
  const [bookmarkVersion, setBookmarkVersion] = useState(0);

  useEffect(() => {
    shell.slides.set(lessonId, index);
  }, [shell, lessonId, index]);

  const isBookmarked = useMemo(() => {
    void bookmarkVersion;
    return bookmarkQueue
      .getQueue()
      .some(
        (b) =>
          b.lessonId === lessonId &&
          b.slideId === index &&
          b.status !== "removal_pending",
      );
  }, [lessonId, index, bookmarkVersion]);

  const handleToggleBookmark = async () => {
    if (isBookmarked) {
      bookmarkQueue.enqueueRemoval({
        lessonId,
        slideId: index,
      });
      setBookmarkVersion((v) => v + 1);
      setToastVisible(false);
      trackBookmarkRemoved({
        targetType: "slide",
        courseId: manifest.course_id,
        lessonId,
        slideId: index,
      });
      try {
        await session.request("/v1/bookmarks/remove", "POST", {
          lesson_id: lessonId,
          slide_id: index,
        });
      } catch {
        // Offline
      }
    } else {
      const queued = bookmarkQueue.enqueue({
        courseId: manifest.course_id,
        courseTitle:
          manifest.course_id === "astronomy"
            ? "Astronomy"
            : manifest.title || "Course",
        lessonId,
        lessonTitle: manifest.title || "Why the sky changes",
        slideId: index,
        slideTitle: `Slide ${index + 1}`,
        targetType: "slide",
      });
      setBookmarkVersion((v) => v + 1);
      setToastMessage("Slide bookmarked");
      setToastVisible(true);
      trackBookmarkAdded({
        targetType: "slide",
        courseId: manifest.course_id,
        lessonId,
        slideId: index,
      });
      try {
        const res = await session.request<{ id: string }>(
          "/v1/bookmarks",
          "POST",
          {
            action_key: queued.actionKey,
            lesson_id: lessonId,
            slide_id: index,
            target_type: "slide",
          },
        );
        if (res?.id) {
          bookmarkQueue.markSynced(queued.actionKey, res.id);
        }
      } catch {
        // Offline
      }
    }
  };

  const handleUndoBookmark = async () => {
    bookmarkQueue.enqueueRemoval({
      lessonId,
      slideId: index,
    });
    setBookmarkVersion((v) => v + 1);
    setToastVisible(false);
    trackBookmarkRemoved({
      targetType: "slide",
      courseId: manifest.course_id,
      lessonId,
      slideId: index,
    });
    try {
      await session.request("/v1/bookmarks/remove", "POST", {
        lesson_id: lessonId,
        slide_id: index,
      });
    } catch {
      // Offline
    }
  };

  useEffect(() => {
    return subscribeSyncStatus(() => {
      setPendingSync(hasPendingSync(owner, lessonId));
    });
  }, [owner, lessonId]);

  useEffect(() => {
    const onOnline = () => {
      if (hasPendingSync(owner, lessonId)) {
        void synchronizeProgress(owner, lessonId)
          .then(() => {
            setPendingSync(hasPendingSync(owner, lessonId));
            setUnsaved(false);
          })
          .catch(() => {});
      }
    };
    window.addEventListener("online", onOnline);
    if (
      typeof navigator !== "undefined" &&
      navigator.onLine &&
      hasPendingSync(owner, lessonId)
    ) {
      onOnline();
    }
    return () => {
      window.removeEventListener("online", onOnline);
    };
  }, [owner, lessonId]);

  const slideDocument = useQuery({
    queryKey: [
      "lesson-document",
      owner,
      lessonId,
      manifest.version,
      slide.slide_id,
    ],
    queryFn: () => loadDocument(slide),
    initialData: () => preparedDocument(slide),
    retry: false,
    networkMode: "always",
  });
  const queryClient = useQueryClient();
  useEffect(() => {
    const nextSlide = manifest.slides[index + 1];
    if (nextSlide) {
      void queryClient.prefetchQuery({
        queryKey: [
          "lesson-document",
          owner,
          lessonId,
          manifest.version,
          nextSlide.slide_id,
        ],
        queryFn: () => loadDocument(nextSlide),
      });
    }
  }, [index, manifest, owner, lessonId, queryClient]);
  const command = useCallback(
    (value: Parameters<typeof sendCommand>[1]) =>
      sendCommand(frame.current?.contentWindow ?? null, value),
    [frame],
  );
  const persist = useCallback(() => {
    const revision = ++saveRevision.current;
    const timeMs = Math.round(clock.snapshot().time);
    const position = {
      version: manifest.version,
      slide_id: slide.slide_id,
      time_ms: timeMs,
    };
    writeLocalPosition(owner, lessonId, position);

    const event = {
      action_key: generateActionKey(),
      lesson_id: lessonId,
      version: manifest.version,
      slide_id: slide.slide_id,
      time_ms: timeMs,
      occurred_at: new Date().toISOString(),
    };

    if (typeof navigator !== "undefined" && !navigator.onLine) {
      enqueueSyncEvent(owner, event);
      setPendingSync(true);
      return;
    }

    void savePosition(owner, lessonId, position).then(
      () => {
        if (revision === saveRevision.current) {
          setUnsaved(false);
          if (hasPendingSync(owner, lessonId)) {
            void synchronizeProgress(owner, lessonId).then(() => {
              setPendingSync(hasPendingSync(owner, lessonId));
            });
          }
        }
      },
      () => {
        if (revision === saveRevision.current) {
          setUnsaved(true);
        }
        trackPlayer("lesson_player_error", {
          lesson_id: lessonId,
          slide_id: slide.slide_id,
          error_code: "position_unavailable",
        });
      },
    );
  }, [clock, owner, lessonId, manifest.version, slide.slide_id]);

  const pause = useCallback(() => {
    const wasPlaying = clock.snapshot().playing;
    clock.pause(performance.now());
    command({
      type: wasPlaying ? "tick" : "seek",
      time: clock.snapshot().time,
    });
    command({ type: "pause" });
    if (wasPlaying)
      trackPlayer("slide_paused", {
        lesson_id: lessonId,
        slide_id: slide.slide_id,
        index,
      });
    persist();
  }, [clock, command, index, lessonId, slide.slide_id, persist]);
  const seek = useCallback(
    (time: number) => {
      clock.seek(time, performance.now());
      command({ type: "seek", time: clock.snapshot().time });
      command({ type: clock.snapshot().playing ? "play" : "pause" });
      persist();
    },
    [clock, command, persist],
  );
  const toggle = useCallback(() => {
    if (clock.snapshot().playing) pause();
    else if (!document.hidden && !panel) {
      clock.play(performance.now());
      command({ type: "play" });
    }
  }, [clock, pause, panel, command]);
  // Opening a panel pauses playback; closing it puts the slide and time back exactly once.
  const opened = useRef<{ index: number; time: number } | null>(null);
  const openPanel = (name: NonNullable<typeof panel>) => {
    pause();
    if (panel === null) opened.current = { index, time: clock.snapshot().time };
    setPanel(name);
  };
  const closePanel = () => {
    const saved = opened.current;
    opened.current = null;
    setPanel(null);
    if (!saved) return;
    if (saved.index !== index) onNavigate(saved.index);
    else if (Math.abs(clock.snapshot().time - saved.time) > 1) seek(saved.time);
  };
  useEffect(() => {
    if (panel === "Ask tutor")
      trackGuidedUi({ event: "ui_sheet_opened", sheetId: "lesson_tutor" });
  }, [panel]);
  useEffect(() => {
    if (slideDocument.error) {
      const isAsset =
        slideDocument.error.message === "asset_unavailable" ||
        slideDocument.error.message.includes("asset") ||
        (slide.assets && slide.assets.length > 0);
      trackPlayer("lesson_player_error", {
        lesson_id: lessonId,
        slide_id: slide.slide_id,
        error_code: isAsset ? "asset_unavailable" : "document_unavailable",
      });
    }
  }, [slideDocument.error, lessonId, slide.slide_id, slide.assets]);
  useEffect(() => {
    command({ type: "set_motion", reduced: reducedMotion });
  }, [command, reducedMotion]);
  useEffect(() => {
    const visibility = () => {
      if (document.hidden) pause();
    };
    const pagehide = () => pause();
    document.addEventListener("visibilitychange", visibility);
    window.addEventListener("pagehide", pagehide);
    return () => {
      document.removeEventListener("visibilitychange", visibility);
      window.removeEventListener("pagehide", pagehide);
      clock.pause(performance.now());
      persist();
    };
  }, [clock, pause, persist]);
  useEffect(() => {
    if (!ready) return;
    let lastSave = performance.now();
    const timer = window.setInterval(() => {
      if (!clock.snapshot().playing) return;
      clock.tick(performance.now());
      command({ type: "tick", time: clock.snapshot().time });
      if (performance.now() - lastSave >= 2000) {
        persist();
        lastSave = performance.now();
      }
    }, 50);
    const listener = (event: MessageEvent) => {
      if (!event.data || typeof event.data !== "object") return;

      if (
        !isSlideMessage(
          event,
          frame.current?.contentWindow ?? null,
          slide.checkpoints.map((c) => c.id),
        )
      )
        return;
      if (event.data.type === "asset_error") {
        setAssetError(true);
        trackPlayer("lesson_player_error", {
          lesson_id: lessonId,
          slide_id: slide.slide_id,
          error_code: "asset_unavailable",
        });
      } else if (event.data.type === "checkpoint") {
        const name = event.data.name;
        const point = slide.checkpoints.find((c) => c.id === name);
        if (
          point &&
          clock.snapshot().time >= point.at_ms &&
          !reached.current.has(name)
        ) {
          reached.current.add(name);
          setCheckpoint(true);
          persist();
        }
      } else if (
        !viewed.current &&
        clock.snapshot().time >= slide.duration_ms
      ) {
        viewed.current = true;
        clock.end();
        command({ type: "pause" });
        trackPlayer("slide_viewed", {
          lesson_id: lessonId,
          slide_id: slide.slide_id,
          index,
          watch_ms: Math.round(clock.snapshot().watch),
          offline: !navigator.onLine,
        });
        persist();
      }

      if (event.data.type === "player_keydown") {
        if (event.data.code === "Space" || event.data.key === " ") {
          toggle();
        } else if (
          event.data.code === "ArrowRight" ||
          event.data.key === "ArrowRight"
        ) {
          seek(clock.snapshot().time + 1000);
        } else if (
          event.data.code === "ArrowLeft" ||
          event.data.key === "ArrowLeft"
        ) {
          seek(clock.snapshot().time - 1000);
        }
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
    return () => {
      clearInterval(timer);
      window.removeEventListener("message", listener);
    };
  }, [
    ready,
    clock,
    command,
    index,
    lessonId,
    slide,
    persist,
    frame,
    seek,
    toggle,
  ]);
  const slideFailed = !!slideDocument.error || assetError;
  const disabled = !ready || slideFailed || panel !== null;
  return (
    <div
      className="player"
      role="region"
      aria-label="Lesson playback"
      tabIndex={0}
      aria-keyshortcuts="Space ArrowLeft ArrowRight"
      onKeyDown={(event) => {
        if (
          disabled ||
          (event.target instanceof HTMLElement &&
            event.target.closest("button, a, input, textarea, select"))
        )
          return;
        if (event.code === "Space") {
          event.preventDefault();
          toggle();
        }
        if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
          event.preventDefault();
          seek(
            clock.snapshot().time + (event.key === "ArrowRight" ? 1000 : -1000),
          );
        }
      }}
    >
      <header className="focused-context">
        <Link className="shell-link" to={exit} onClick={pause}>
          Exit lesson
        </Link>
        <span>Course · Lesson</span>
      </header>
      <h1>{manifest.title}</h1>
      <div
        className="progress"
        role="progressbar"
        aria-label="Lesson slides"
        aria-valuemin={0}
        aria-valuemax={manifest.slides.length}
        aria-valuenow={index + 1}
      >
        <span
          style={{ width: `${((index + 1) / manifest.slides.length) * 100}%` }}
        />
      </div>
      <p>
        Slide {index + 1} of {manifest.slides.length}
      </p>
      <p role="status">
        {availableOffline(manifest.lesson_id)
          ? "Available offline"
          : "Offline download unavailable. Keep this lesson open or retry online."}
      </p>
      {pendingSync ? (
        <div role="status" className="player-sync-status">
          <p>Waiting to sync</p>
          <button
            className="duo-button quiet"
            onClick={() => {
              void synchronizeProgress(owner, lessonId)
                .then(() => {
                  setPendingSync(hasPendingSync(owner, lessonId));
                  setUnsaved(false);
                })
                .catch(() => {});
            }}
          >
            Retry sync
          </button>
        </div>
      ) : unsaved ? (
        <div role="status">
          <p>Position saved on this device only.</p>
          <button className="duo-button quiet" onClick={persist}>
            Retry saving position
          </button>
        </div>
      ) : null}
      <PanelLayout
        panel={
          panel === "Quick note" ? (
            <QuickNotePanel
              open={true}
              onClose={closePanel}
              context={{
                source: "slide",
                courseId: manifest.course_id,
                lessonId,
                slideId: index,
                slideTitle: `Slide ${index + 1} · ${manifest.title || "Why the sky changes"}`,
              }}
            />
          ) : (
            <ResponsivePanel
              title={panel ?? "Transcript"}
              open={panel !== null}
              onClose={closePanel}
            >
              <p className="feedback badge-warning" role="status">
                Lesson paused
              </p>
              {panel === "Transcript" && (
                <div className="transcript-list" aria-label="Lesson transcript">
                  {manifest.slides.map((s, idx) => (
                    <div
                      key={s.slide_id}
                      className={`transcript-entry ${idx === index ? "active" : ""}`}
                    >
                      <b>Slide {idx + 1}. </b>
                      <span>{s.explanation || s.narration_text}</span>
                    </div>
                  ))}
                </div>
              )}
              {panel === "Ask tutor" && (
                <TutorChat
                  courseId={manifest.course_id}
                  lesson={{ lesson_id: lessonId, slide_index: index }}
                  embedded
                  onClose={closePanel}
                  onReadTranscript={() => setPanel("Transcript")}
                />
              )}
            </ResponsivePanel>
          )
        }
      >
        {slideDocument.isPending && <p role="status">Loading slide…</p>}
        {slideFailed ? (
          <div
            className="stage-recovery"
            role="alert"
            aria-label="Slide load failure"
          >
            <span className="recovery-icon" aria-hidden="true">
              <svg
                viewBox="0 0 24 24"
                width="40"
                height="40"
                fill="none"
                stroke="currentColor"
                strokeWidth="2.5"
                strokeLinecap="round"
                strokeLinejoin="round"
              >
                <path d="M12 3 2 20.5h20z" />
                <path d="M12 10v4.5M12 17.5h.01" />
              </svg>
            </span>
            <b className="recovery-title">This slide didn't load</b>
            <span className="help recovery-explanation">
              {assetError ||
              slideDocument.error?.message === "asset_unavailable" ||
              (slide.assets && slide.assets.length > 0)
                ? "A diagram asset is missing. Your place is saved."
                : "This slide is unavailable. Check your connection and retry. Your place is saved."}
            </span>
            <div className="recovery-actions">
              <button
                className="duo-button primary"
                type="button"
                onClick={() => {
                  trackPlayerRecovery("retry", "asset_unavailable");
                  setAssetError(false);
                  void slideDocument.refetch();
                }}
              >
                Retry slide
              </button>
              <button
                className="duo-button sec"
                type="button"
                onClick={() => {
                  trackPlayerRecovery("exit", "asset_unavailable");
                  pause();
                  void navigate({ to: exit });
                }}
              >
                Exit to path
              </button>
            </div>
            <span className="help recovery-transcript-hint">
              Transcript for slide {index + 1} is still available below the
              frame.
            </span>
          </div>
        ) : (
          slideDocument.data && (
            <iframe
              ref={frame}
              title={`Slide ${index + 1}`}
              sandbox="allow-scripts"
              srcDoc={sandboxDocument(
                (manifest.cast && slide.segments?.length
                  ? generatedCharacterDocument(
                      {
                        slide_id: slide.slide_id,
                        duration_ms: slide.duration_ms,
                        segments: slide.segments,
                      },
                      manifest.cast,
                      reducedMotion,
                      typeof navigator !== "undefined" && !navigator.onLine,
                    )
                  : "") + slideDocument.data,
                manifest.asset_origin,
                reducedMotion,
                slide,
                availableOffline(manifest.lesson_id),
              )}
              referrerPolicy="no-referrer"
              onLoad={() => {
                setReady(true);
                onStart();
                command({ type: "set_motion", reduced: reducedMotion });
                command({ type: "rate", rate });
                command({ type: "seek", time: clock.snapshot().time });
                if (autoplay && !document.hidden && !panel) {
                  clock.play(performance.now());
                  command({ type: "play" });
                } else command({ type: "pause" });
                if (typeof navigator !== "undefined" && !navigator.onLine) {
                  persist();
                }
              }}
            />
          )
        )}
        {captionsVisible && (
          <p className="slide-caption">
            {slide.explanation || slide.narration_text}
          </p>
        )}
        <p role="status">
          {slideFailed
            ? "Slide paused"
            : playback.ended
              ? "Slide ended. Continue when you’re ready."
              : checkpoint
                ? "Checkpoint reached"
                : playback.playing
                  ? "Playing"
                  : "Paused"}
        </p>
        <div className="controls player-controls">
          <button
            onClick={() => {
              pause();
              onNavigate(index - 1);
            }}
            disabled={index === 0 || panel !== null}
          >
            Previous
          </button>
          <button
            aria-label="Replay slide"
            disabled={disabled}
            onClick={() => {
              viewed.current = false;
              reached.current.clear();
              setCheckpoint(false);
              clock.replay(performance.now());
              command({ type: "seek", time: 0 });
              command({ type: "play" });
              trackPlayer("slide_replayed", {
                lesson_id: lessonId,
                slide_id: slide.slide_id,
                index,
              });
              persist();
            }}
          >
            Replay
          </button>
          <button disabled={disabled || playback.ended} onClick={toggle}>
            {playback.playing ? "Pause" : "Play"}
          </button>
          <div className="audio-timeline">
            <input
              className="audio-timeline-slider"
              type="range"
              min={0}
              max={slide.duration_ms}
              step={100}
              value={Math.round(playback.time)}
              aria-label="Slide time"
              aria-valuetext={`${(playback.time / 1000).toFixed(1)} of ${slide.duration_ms / 1000} seconds`}
              disabled={disabled}
              onChange={(event) => seek(Number(event.currentTarget.value))}
            />
            <span aria-hidden="true">
              {(playback.time / 1000).toFixed(1)} / {slide.duration_ms / 1000}s
            </span>
          </div>
          <button
            aria-label={`Speed ${rate}x`}
            disabled={disabled}
            onClick={() => {
              const next = speeds[(speeds.indexOf(rate) + 1) % speeds.length];
              clock.rate(next, performance.now());
              onRate(next);
              command({ type: "rate", rate: next });
            }}
          >
            {rate}×
          </button>
          <button
            aria-label={muted ? "Unmute narration" : "Mute narration"}
            aria-pressed={muted}
            disabled={slideFailed}
            onClick={() => onMute(!muted)}
          >
            {muted ? "Unmute" : "Mute"}
          </button>
          {slideFailed ? (
            <button disabled className="duo-button disabled">
              Next · unavailable
            </button>
          ) : index === manifest.slides.length - 1 ? (
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
              onClick={() => {
                pause();
                onNavigate(index + 1);
              }}
              disabled={panel !== null}
            >
              Next
            </button>
          )}
        </div>
        <div className="panel-tools" aria-label="Lesson tools">
          <button
            className={`duo-button quiet ${isBookmarked ? "on" : ""}`}
            type="button"
            aria-pressed={isBookmarked}
            onClick={handleToggleBookmark}
          >
            {isBookmarked ? "Saved" : "Bookmark"}
          </button>
          <button
            className={`duo-button quiet tool-captions ${captionsVisible ? "on" : ""}`}
            type="button"
            aria-pressed={captionsVisible}
            onClick={() => setCaptionsVisible(!captionsVisible)}
          >
            Captions
          </button>
          {(["Transcript", "Ask tutor", "Quick note"] as const).map((name) => (
            <button
              className={`duo-button quiet ${panel === name ? "on" : ""}`}
              key={name}
              aria-expanded={panel === name}
              onClick={() => openPanel(name)}
            >
              {name}
            </button>
          ))}
        </div>
      </PanelLayout>
      {toastVisible && (
        <div
          className="toast"
          role="status"
          style={{
            position: "fixed",
            bottom: 24,
            left: "50%",
            transform: "translateX(-50%)",
            background: "#2B2B2B",
            color: "#fff",
            padding: "12px 20px",
            borderRadius: 14,
            display: "flex",
            alignItems: "center",
            gap: 12,
            zIndex: 1000,
            boxShadow: "0 4px 12px rgba(0,0,0,0.2)",
          }}
        >
          <span style={{ fontSize: 16 }}>🔖</span>
          <span style={{ flex: 1, fontWeight: 700 }}>{toastMessage}</span>
          <button
            type="button"
            onClick={handleUndoBookmark}
            style={{
              color: "#9FD3FF",
              background: "none",
              border: "none",
              fontWeight: 800,
              cursor: "pointer",
              padding: 0,
            }}
          >
            Undo
          </button>
        </div>
      )}
    </div>
  );
}
