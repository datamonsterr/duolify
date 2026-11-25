import {
  useCallback,
  useEffect,
  useMemo,
  useReducer,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { Link } from "@tanstack/react-router";
import {
  trackImmersive,
  type immersiveControls,
  type immersiveVias,
} from "../../../lib/analytics";
import { AUTO_HIDE_MS, canAutoHide, createAutoHide } from "./autohide";
import { Icon } from "../../design/Icon";
import { useUiPreferences } from "../../design/localization/Preferences";
import { useImmersiveExtensions } from "./extensions";
import { clientToStage } from "./geometry";
import { createGestureRecognizer } from "./gestures";
import type { ImmersiveActions, ImmersiveView } from "./types";
import { dispatchControls, focusMemory, useControls, useLayout } from "./store";
import { speedLabel, formatClock } from "./format";
import { useWakeLock } from "./useWakeLock";
import { useLatest } from "./useLatest";
import "./immersive.css";

type Control = (typeof immersiveControls)[number];
type Via = (typeof immersiveVias)[number];
const COUNTDOWN_S = 3;
const HOLD_TO_UNLOCK_MS = 700;

type Props = {
  view: ImmersiveView;
  actions: ImmersiveActions;
  /** Where "Take lesson test" leads after the last Slide. */
  lastSlide: boolean;
  lessonId: string;
};

export default function ImmersiveLayer({
  view,
  actions,
  lastSlide,
  lessonId,
}: Props) {
  const { t } = useUiPreferences();
  const ext = useImmersiveExtensions();
  const controls = useControls();
  const layout = useLayout();
  const ui = useRef<HTMLDivElement>(null);
  const [focusInside, setFocusInside] = useState(false);
  const [tick, bump] = useReducer((n: number) => n + 1, 0);
  const [railOpen, setRailOpen] = useState(false);
  const [moreOpen, setMoreOpen] = useState(false);
  const [announce, setAnnounce] = useState("");
  const [assistive, setAssistive] = useState(focusMemory.keyboard);

  const latest = useLatest({
    view,
    actions,
    ext,
    layout,
    locked: controls.locked,
  });

  const used = useCallback((control: Control, via: Via) => {
    trackImmersive({ event: "immersive_control_used", control, via });
  }, []);
  const viaOf = (e: { detail: number }): Via => {
    if (e.detail === 0) {
      focusMemory.set({ keyboard: true });
      setAssistive(true);
      return "keyboard";
    }
    return "button";
  };

  // Announce the Slide the way the framed player names it.
  useEffect(() => {
    const t = setTimeout(
      () => setAnnounce(`Slide ${view.index + 1} of ${view.slides.length}`),
      50,
    );
    return () => clearTimeout(t);
  }, [view.index, view.slides.length]);

  const controlsVisibleRef = useLatest(controls.visible);
  // Keep the screen awake only while a Slide plays.
  useWakeLock(view.playing);

  // Keyboard users keep their place after the Slide changes.
  useEffect(() => {
    const id = focusMemory.id;
    if (!id || !focusMemory.keyboard) return;
    const el = ui.current?.querySelector<HTMLElement>(`[data-imm="${id}"]`);
    if (el && !ui.current?.contains(document.activeElement)) el.focus();
  }, []);

  // Stopped at the end of a Slide: keep next / replay on screen.
  useEffect(() => {
    if (view.ended) dispatchControls({ type: "show" });
  }, [view.ended]);

  // Auto-hide.
  const hider = useMemo(
    () => createAutoHide(() => dispatchControls({ type: "hide" })),
    [],
  );
  useEffect(() => {
    hider.arm(
      controls.visible &&
        canAutoHide({
          focusInside,
          assistive,
          stoppedAtEnd: view.ended,
          locked: controls.locked,
          pinned: controls.list,
        }),
    );
    return hider.dispose;
  }, [hider, controls, focusInside, assistive, view.ended, tick]);

  // Gestures. Everything below the layer is the Slide, which never gets touches.
  const rec = useMemo(
    () =>
      createGestureRecognizer(
        {
          onTap: () => {
            if (latest.current.ext.onStageTap?.()) return;
            dispatchControls({ type: "toggle" });
          },
          onDoubleTap: (side) =>
            step(side === "left" ? "previous" : "next", "double_tap"),
          onSwipe: (dir) => step(dir, "swipe"),
          onRegionGestureStart: (r) =>
            latest.current.ext.onRegionGestureStart?.(r),
          onRegionGestureUpdate: (r) =>
            latest.current.ext.onRegionGestureUpdate?.(r),
          onRegionGestureEnd: (r, i) =>
            latest.current.ext.onRegionGestureEnd?.(r, i),
        },
        () => ({
          w: latest.current.layout.width,
          h: latest.current.layout.height,
        }),
      ),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [],
  );
  // Region callbacks are optional; without them the recogniser still swallows the spread.
  function step(dir: "previous" | "next", via: Via) {
    const { view: v, actions: a } = latest.current;
    if (dir === "previous" && v.index > 0) {
      used("previous", via);
      a.previous();
    } else if (dir === "next" && v.index < v.slides.length - 1) {
      used("next", via);
      a.next();
    }
  }
  useEffect(() => () => rec.dispose(), [rec]);
  const point = (e: React.PointerEvent) => {
    const l = latest.current.layout;
    return clientToStage(e.clientX, e.clientY, l.vw, l.vh, l);
  };

  // Controls the learner can reach without a pointer.
  const onUiKeyDown = (e: React.KeyboardEvent) => {
    bump();
    if (e.key === "Tab") {
      focusMemory.set({ keyboard: true });
      setAssistive(true);
    }
  };
  const act = (control: Control, fn: () => void) => (e: React.MouseEvent) => {
    used(control, viaOf(e));
    bump();
    fn();
  };
  const remember = (id: string) => () => {
    focusMemory.set({ id });
  };

  // Show the layer when a keyboard user starts tabbing while it is hidden.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Tab" || latest.current.locked || e.defaultPrevented)
        return;
      // Inside the composer or answer panel Tab keeps its normal order.
      if ((e.target as Element | null)?.closest?.(".imm-answer-ui, .imm-notes"))
        return;
      focusMemory.set({ keyboard: true });
      setAssistive(true);
      if (!controlsVisibleRef.current) {
        e.preventDefault();
        dispatchControls({ type: "show" });
        requestAnimationFrame(() =>
          ui.current?.querySelector<HTMLElement>('[data-imm="play"]')?.focus(),
        );
      }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [latest, controlsVisibleRef]);

  // After locking, move focus to the unlock target.
  useEffect(() => {
    if (controls.locked && focusMemory.keyboard)
      ui.current?.querySelector<HTMLElement>('[data-imm="unlock"]')?.focus();
  }, [controls.locked]);

  const total = view.slides.reduce((n, s) => n + s.durationMs, 0);
  const before = view.slides
    .slice(0, view.index)
    .reduce((n, s) => n + s.durationMs, 0);
  const elapsed = before + Math.min(view.timeMs, view.durationMs);
  const uiStyle = layout.rotated
    ? {
        width: layout.vh,
        height: layout.vw,
        marginLeft: -layout.vh / 2,
        marginTop: -layout.vw / 2,
      }
    : undefined;

  return (
    <div className="imm-layer" data-testid="immersive-layer">
      <div
        className="imm-gestures"
        data-testid="immersive-gesture-layer"
        aria-hidden="true"
        onPointerDown={(e) => {
          if (latest.current.locked) return;
          e.currentTarget.setPointerCapture?.(e.pointerId);
          const p = point(e);
          rec.down(e.pointerId, p.x, p.y, e.timeStamp);
        }}
        onPointerMove={(e) => {
          if (latest.current.locked) return;
          const p = point(e);
          rec.move(e.pointerId, p.x, p.y);
        }}
        onPointerUp={(e) => {
          if (latest.current.locked) return;
          const p = point(e);
          rec.up(e.pointerId, p.x, p.y, e.timeStamp);
        }}
        onPointerCancel={(e) => rec.cancel(e.pointerId)}
        onContextMenu={(e) => e.preventDefault()}
      />
      {view.captions && view.caption && !view.failed && (
        <p
          className="imm-caption"
          data-testid="immersive-caption"
          data-raised={controls.visible || undefined}
        >
          {view.caption}
        </p>
      )}
      <div
        ref={ui}
        className="imm-ui"
        data-rotated={layout.rotated || undefined}
        style={uiStyle}
        onKeyDown={onUiKeyDown}
        onPointerDownCapture={bump}
      >
        <p className="imm-sr" role="status" data-testid="immersive-announcer">
          {announce}
        </p>
        <p className="imm-sr" role="status" data-testid="immersive-state">
          {view.failed
            ? "Slide paused"
            : view.ended
              ? "Slide ended. Continue when you’re ready."
              : view.playing
                ? "Playing"
                : "Paused"}
        </p>
        {controls.locked ? (
          <UnlockTarget
            onUnlock={() => {
              used("unlock", "button");
              dispatchControls({ type: "unlock" });
            }}
          />
        ) : (
          <>
            <div
              className="imm-controls"
              data-testid="immersive-controls"
              data-visible={controls.visible}
              role="group"
              aria-label="Immersive lesson controls"
              inert={!controls.visible}
              onFocus={(e) =>
                // Only keyboard / switch focus keeps the layer up: a tap or click
                // leaves focus on the button without :focus-visible.
                setFocusInside(
                  (e.target as HTMLElement).matches(":focus-visible"),
                )
              }
              onBlur={(e) => {
                if (!e.currentTarget.contains(e.relatedTarget as Node | null))
                  setFocusInside(false);
              }}
            >
              <div className="imm-top">
                <Btn
                  id="exit"
                  label="Exit immersive mode"
                  onFocus={remember("exit")}
                  onClick={() => actions.exit()}
                >
                  ✕
                </Btn>
                <span className="imm-title" data-testid="immersive-title">
                  {view.title}
                </span>
                <Btn
                  id="bookmark"
                  label={
                    view.bookmarked ? "Remove bookmark" : "Bookmark this slide"
                  }
                  pressed={view.bookmarked}
                  onFocus={remember("bookmark")}
                  onClick={act("bookmark", actions.toggleBookmark)}
                >
                  {view.bookmarked ? "★" : "☆"}
                </Btn>
              </div>
              <div className="imm-centre">
                <Btn
                  id="previous"
                  label="Previous slide"
                  disabled={view.index === 0}
                  onFocus={remember("previous")}
                  onClick={act("previous", actions.previous)}
                >
                  ⏮
                </Btn>
                <Btn
                  id="play"
                  big
                  label={view.playing ? "Pause" : "Play"}
                  disabled={view.disabled || view.ended}
                  onFocus={remember("play")}
                  onClick={(e) => {
                    used(view.playing ? "pause" : "play", viaOf(e));
                    bump();
                    actions.toggle();
                  }}
                >
                  {view.playing ? "⏸" : "▶"}
                </Btn>
                {lastSlide ? (
                  <Link
                    className="imm-btn"
                    data-imm="next"
                    to="/lesson/$lessonId/test"
                    params={{ lessonId }}
                    data-testid="immersive-take-test"
                    onClick={actions.pause}
                  >
                    <span aria-hidden="true">🏁</span>
                    <span className="imm-sr">Take lesson test</span>
                  </Link>
                ) : (
                  <Btn
                    id="next"
                    label="Next slide"
                    onFocus={remember("next")}
                    onClick={act("next", actions.next)}
                  >
                    ⏭
                  </Btn>
                )}
              </div>
              <div className="imm-bottom">
                <div
                  className="imm-progress"
                  data-testid="immersive-progress"
                  role="progressbar"
                  aria-label="Lesson progress"
                  aria-valuemin={0}
                  aria-valuemax={Math.round(total / 1000)}
                  aria-valuenow={Math.round(elapsed / 1000)}
                  aria-valuetext={`Slide ${view.index + 1} of ${view.slides.length}, ${formatClock(elapsed)} of ${formatClock(total)}`}
                >
                  {view.slides.map((s, i) => (
                    <span
                      key={s.id}
                      className="imm-seg"
                      data-testid="immersive-segment"
                      data-state={
                        i < view.index
                          ? "done"
                          : i === view.index
                            ? "current"
                            : "todo"
                      }
                      style={{ flexGrow: Math.max(1, s.durationMs) }}
                    >
                      <span
                        style={{
                          width: `${
                            i < view.index
                              ? 100
                              : i === view.index
                                ? Math.min(
                                    100,
                                    (view.timeMs /
                                      Math.max(1, view.durationMs)) *
                                      100,
                                  )
                                : 0
                          }%`,
                        }}
                      />
                    </span>
                  ))}
                </div>
                <div className="imm-row">
                  <div className="imm-playback">
                    <Btn
                      id="replay"
                      label="Replay this slide"
                      disabled={view.disabled}
                      onFocus={remember("replay")}
                      onClick={act("replay_slide", actions.replay)}
                    >
                      ↻
                    </Btn>
                    <Btn
                      id="autoplay"
                      label={view.autoAdvance ? "Autoplay on" : "Autoplay off"}
                      pressed={view.autoAdvance}
                      onFocus={remember("autoplay")}
                      onClick={act(
                        view.autoAdvance ? "autoplay_off" : "autoplay_on",
                        () => actions.setAutoAdvance(!view.autoAdvance),
                      )}
                    >
                      <Icon name="autoplay" />{" "}
                      {view.autoAdvance ? "Auto" : "Manual"}
                    </Btn>
                    <Btn
                      id="speed"
                      label={`Speed ${speedLabel(view.rate)}`}
                      disabled={view.disabled}
                      onFocus={remember("speed")}
                      onClick={act("speed", actions.cycleSpeed)}
                    >
                      {speedLabel(view.rate)}
                    </Btn>
                    <span
                      className="imm-time"
                      data-testid="immersive-time"
                      aria-hidden="true"
                    >
                      {formatClock(elapsed)} / {formatClock(total)}
                    </span>
                  </div>
                  <div className="imm-tools">
                    <button
                      type="button"
                      className="imm-btn imm-more"
                      aria-label={t("player.more_controls")}
                      aria-expanded={moreOpen}
                      onClick={() => setMoreOpen(!moreOpen)}
                    >
                      ⋯
                    </button>
                    <div
                      className="imm-overflow"
                      data-open={moreOpen || undefined}
                    >
                      <Btn
                        id="captions"
                        label="Captions"
                        pressed={view.captions}
                        onFocus={remember("captions")}
                        onClick={act("captions", actions.toggleCaptions)}
                      >
                        CC
                      </Btn>
                      <Btn
                        id="narration"
                        label="Narration"
                        pressed={!view.muted}
                        onFocus={remember("narration")}
                        onClick={act("narration", actions.toggleMute)}
                      >
                        <Icon name={view.muted ? "mute" : "volume"} />
                      </Btn>
                      <Btn
                        id="slides"
                        label="Slide list"
                        expanded={controls.list}
                        onFocus={remember("slides")}
                        onClick={act("slide_list", () =>
                          dispatchControls({
                            type: controls.list ? "closeList" : "openList",
                          }),
                        )}
                      >
                        ☰
                      </Btn>
                      <Btn
                        id="rotate"
                        label="Go full screen when I turn my phone"
                        pressed={view.fullscreenOnRotate}
                        onFocus={remember("rotate")}
                        onClick={(e) => {
                          viaOf(e);
                          bump();
                          actions.setFullscreenOnRotate(
                            !view.fullscreenOnRotate,
                          );
                        }}
                      >
                        ⟳
                      </Btn>
                      <Btn
                        id="lock"
                        label="Lock controls"
                        onFocus={remember("lock")}
                        onClick={act("lock", () =>
                          dispatchControls({ type: "lock" }),
                        )}
                      >
                        <Icon name="lock" />
                      </Btn>
                    </div>
                  </div>
                </div>
              </div>
              {controls.list && (
                <div
                  className="imm-list"
                  role="group"
                  aria-label="Slides"
                  data-testid="immersive-slide-list"
                  data-imm-list-open
                >
                  <ol>
                    {view.slides.map((s, i) => (
                      <li key={s.id}>
                        <button
                          type="button"
                          className="imm-btn imm-list-item"
                          aria-current={i === view.index ? "true" : undefined}
                          onClick={() => {
                            bump();
                            actions.goTo(i);
                          }}
                        >
                          <b>{i + 1}.</b> {s.label}
                        </button>
                      </li>
                    ))}
                  </ol>
                </div>
              )}
            </div>
            {view.ended &&
              view.autoAdvance &&
              !ext.busy &&
              !lastSlide &&
              !view.failed && (
                <EndCountdown
                  reducedMotion={view.reducedMotion}
                  onDone={actions.next}
                  onCancel={bump}
                />
              )}
            <SideRail open={railOpen} onToggle={() => setRailOpen((o) => !o)}>
              {ext.sideRail}
            </SideRail>
          </>
        )}
      </div>
    </div>
  );
}

function Btn({
  id,
  label,
  children,
  onClick,
  onFocus,
  disabled,
  pressed,
  expanded,
  big,
}: {
  id: string;
  label: string;
  children: ReactNode;
  onClick: (e: React.MouseEvent) => void;
  onFocus?: () => void;
  disabled?: boolean;
  pressed?: boolean;
  expanded?: boolean;
  big?: boolean;
}) {
  return (
    <button
      type="button"
      className={`imm-btn${big ? " imm-btn-big" : ""}`}
      data-imm={id}
      data-testid={`immersive-${id}`}
      aria-label={label}
      aria-pressed={pressed}
      aria-expanded={expanded}
      disabled={disabled}
      onFocus={onFocus}
      onClick={onClick}
    >
      <span aria-hidden="true">{children}</span>
    </button>
  );
}

function SideRail({
  open,
  onToggle,
  children,
}: {
  open: boolean;
  onToggle: () => void;
  children: ReactNode;
}) {
  return (
    <aside
      className="imm-rail"
      data-testid="immersive-side-rail"
      aria-label="Lesson tools"
      data-open={open || undefined}
    >
      <button
        type="button"
        className="imm-btn imm-rail-handle"
        data-testid="immersive-side-rail-handle"
        aria-expanded={open}
        aria-controls="immersive-side-rail-slot"
        aria-label="Lesson tools"
        onClick={onToggle}
      >
        <span aria-hidden="true">{open ? "›" : "‹"}</span>
      </button>
      <div
        id="immersive-side-rail-slot"
        className="imm-rail-slot"
        data-testid="immersive-side-rail-slot"
        hidden={!open}
      >
        {children}
      </div>
    </aside>
  );
}

function UnlockTarget({ onUnlock }: { onUnlock: () => void }) {
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const [holding, setHolding] = useState(false);
  const stop = () => {
    clearTimeout(timer.current);
    timer.current = undefined;
    setHolding(false);
  };
  const start = () => {
    if (timer.current) return;
    setHolding(true);
    timer.current = setTimeout(() => {
      timer.current = undefined;
      setHolding(false);
      onUnlock();
    }, HOLD_TO_UNLOCK_MS);
  };
  useEffect(() => () => clearTimeout(timer.current), []);
  return (
    <button
      type="button"
      className="imm-btn imm-unlock"
      data-imm="unlock"
      data-testid="immersive-unlock"
      data-holding={holding || undefined}
      aria-label="Hold to unlock"
      onPointerDown={start}
      onPointerUp={stop}
      onPointerLeave={stop}
      onPointerCancel={stop}
      onKeyDown={(e) => {
        if ((e.key === "Enter" || e.key === " ") && !e.repeat) {
          e.preventDefault();
          start();
        }
      }}
      onKeyUp={stop}
      onBlur={stop}
      onContextMenu={(e) => e.preventDefault()}
    >
      <Icon name="lock" />
      <span className="imm-unlock-hint" aria-hidden="true">
        Hold to unlock
      </span>
    </button>
  );
}

export { AUTO_HIDE_MS };

/** Mounted only while a Slide is stopped at its end, so a cancel lasts exactly until the next replay or Slide. */
function EndCountdown(props: {
  reducedMotion: boolean;
  onDone: () => void;
  onCancel: () => void;
}) {
  const [cancelled, setCancelled] = useState(false);
  if (cancelled) return null;
  return (
    <Countdown
      {...props}
      onCancel={() => {
        setCancelled(true);
        props.onCancel();
      }}
    />
  );
}

function Countdown({
  reducedMotion,
  onDone,
  onCancel,
}: {
  reducedMotion: boolean;
  onDone: () => void;
  onCancel: () => void;
}) {
  // One-second steps: the count never depends on an animation running.
  const [count, setCount] = useState(COUNTDOWN_S);
  const done = useLatest(onDone);
  useEffect(() => {
    if (count <= 0) {
      done.current();
      return;
    }
    const t = setTimeout(() => setCount((c) => c - 1), 1000);
    return () => clearTimeout(t);
  }, [count, done]);
  return (
    <div
      className="imm-countdown"
      role="group"
      aria-label="Autoplay countdown"
      data-testid="immersive-countdown"
    >
      <span className="imm-count" data-testid="immersive-countdown-count">
        Next slide in {Math.max(count, 0)}
      </span>
      {!reducedMotion && (
        <span
          className="imm-ring"
          data-testid="immersive-countdown-ring"
          aria-hidden="true"
          style={{ animationDuration: `${COUNTDOWN_S}s` }}
        />
      )}
      <button
        type="button"
        className="imm-btn"
        data-testid="immersive-countdown-cancel"
        onClick={onCancel}
      >
        Cancel
      </button>
    </div>
  );
}
