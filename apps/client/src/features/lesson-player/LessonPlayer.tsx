import { useUiPreferences } from "../design/localization/Preferences";
import {
  useCallback,
  useEffect,
  useRef,
  useState,
  useSyncExternalStore,
} from "react";
import { Link } from "@tanstack/react-router";
import { trackPlayer, trackPlayerRecovery } from "../../lib/analytics";
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
import { isSlideMessage, sendCommand } from "./bridge";
import { PanelLayout, ResponsivePanel } from "../shell/AdaptiveLayouts";
import { useShellSession } from "../shell/ShellSession";
const speeds = [1, 1.25, 1.5, 2, 0.75];

export function LessonPlayer({
  lessonId = "sky",
  returnTo,
  targetSlideId,
}: {
  lessonId?: string;
  returnTo?: string;
  targetSlideId?: string;
} = {}) {
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
    />
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
}: {
  manifest: Manifest;
  returnTo?: string;
  targetSlideId?: string;
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
    />
  );
}
function ManifestPlayback({
  manifest,
  returnTo,
  initial,
  restoreError,
  targetSlideId,
}: {
  manifest: Manifest;
  returnTo?: string;
  initial: Position | null;
  restoreError: boolean;
  targetSlideId?: string;
}) {
  const [index, setIndex] = useState(() => {
    if (targetSlideId) {
      const targetIdx = manifest.slides.findIndex(
        (s) => s.slide_id === targetSlideId,
      );
      if (targetIdx !== -1) return targetIdx;
      return 0;
    }
    return Math.max(
      0,
      manifest.slides.findIndex((s) => s.slide_id === initial?.slide_id),
    );
  });
  const [rate, setRate] = useState(1);
  const [note, setNote] = useState("");
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
      note={note}
      onNote={setNote}
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
  note,
  onNote,
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
  note: string;
  onNote: (note: string) => void;
  muted: boolean;
  onMute: (muted: boolean) => void;
}) {
  const lessonId = manifest.lesson_id,
    slide = manifest.slides[index];
  const owner = session.current()?.id ?? "current";
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
  const [panel, setPanel] = useState<
    "Transcript" | "Ask tutor" | "Quick note" | null
  >(null);
  const viewed = useRef(initialTime === slide.duration_ms);
  const reached = useRef(new Set<string>());
  const saveRevision = useRef(0);

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
  const seek = (time: number) => {
    clock.seek(time, performance.now());
    command({ type: "seek", time: clock.snapshot().time });
    command({ type: clock.snapshot().playing ? "play" : "pause" });
    persist();
  };
  const toggle = () => {
    if (clock.snapshot().playing) pause();
    else if (!document.hidden && !panel) {
      clock.play(performance.now());
      command({ type: "play" });
    }
  };
  useEffect(() => {
    if (slideDocument.error)
      trackPlayer("lesson_player_error", {
        lesson_id: lessonId,
        slide_id: slide.slide_id,
        error_code: "document_unavailable",
      });
  }, [slideDocument.error, lessonId, slide.slide_id]);
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
      if (
        !isSlideMessage(
          event,
          frame.current?.contentWindow ?? null,
          slide.checkpoints.map((c) => c.id),
        )
      )
        return;
      if (event.data.type === "checkpoint") {
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
    };
    window.addEventListener("message", listener);
    return () => {
      clearInterval(timer);
      window.removeEventListener("message", listener);
    };
  }, [ready, clock, command, index, lessonId, slide, persist, frame]);
  const [documentMotion] = useState(reducedMotion);
  const disabled = !ready || !!slideDocument.error || panel !== null;
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
                  onChange={(event) => onNote(event.target.value)}
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
            ref={frame}
            title={`Slide ${index + 1}`}
            sandbox="allow-scripts"
            srcDoc={sandboxDocument(
              slideDocument.data,
              manifest.asset_origin,
              documentMotion,
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
        )}
        <p className="slide-caption">{slide.narration_text}</p>
        <p role="status">
          {playback.ended
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
            onClick={() => onMute(!muted)}
          >
            {muted ? "Unmute" : "Mute"}
          </button>
          {index === manifest.slides.length - 1 ? (
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
    </div>
  );
}
