import { useEffect, useRef, useState, type ReactNode } from "react";
import { VoiceBubble, type VoiceBubbleControls } from "../../voice/VoiceBubble";
import { useVoiceQuickNote } from "../../notes/useVoiceQuickNote";
import { ToastHost } from "../../../components/toast";
import { Icon } from "../../design/Icon";
import { useUiPreferences } from "../../design/localization/Preferences";
import { QuickNotePanel } from "../../notes/QuickNotePanel";
import { TutorChat } from "../../chat/TutorChat";
import { ResponsivePanel } from "../../shell/AdaptiveLayouts";
import { trackRegion } from "../../../lib/analytics";
import AnswerPanel, { type PanelOutcome } from "./AnswerPanel";
import { canDock, panelWidth, stageShift } from "./panelLayout";
import type { NoteAnchor } from "../../notes/note-queue";
import {
  ImmersiveExtensionsContext,
  type ImmersiveExtensions,
} from "./extensions";
import type { RegionRect } from "./gestures";
import { RegionOutline } from "./RegionOutline";
import { RegionSelector } from "./RegionSelector";
import { StageSpace } from "./StageSpace";
import { dispatchControls, useLayout } from "./store";
import type { ImmersiveActions, ImmersiveView } from "./types";
import { useLatest } from "./useLatest";
import { useRegionSelection } from "./useRegionSelection";

type Props = {
  view: ImmersiveView;
  actions: ImmersiveActions;
  /** A region to keep outlined ("show in Lesson" from a note). */
  highlight?: RegionRect;
  slideTitle: string;
  children: ReactNode;
};

/**
 * Anchored Quick notes inside immersive mode: the side-rail "Select area" and "Quick note"
 * controls, the region overlay, and the note composer that carries the Slide, the
 * playback time and the region. It wraps the immersive layer and supplies its extension
 * points; the tutor panel (group 4) plugs into the same `useRegionSelection` state.
 */
export default function ImmersiveNotes({
  view,
  actions,
  highlight,
  slideTitle,
  children,
}: Props) {
  const layout = useLayout();
  const latest = useLatest({ view, actions, slideTitle });
  // Playback this component paused, so closing resumes only what it stopped.
  const pausedByUs = useRef(false);
  const [announce, setAnnounce] = useState("");
  const [composer, setComposer] = useState<{
    timeMs: number;
    rect?: RegionRect;
    slideIndex: number;
  } | null>(null);
  const [autoFocusRegion, setAutoFocusRegion] = useState(false);
  const [answer, setAnswer] = useState<{
    rect?: RegionRect;
    atMs: number;
    slideIndex: number;
  } | null>(null);
  const uiWidth = layout.rotated ? layout.vh : layout.vw;
  const docked = canDock(uiWidth);
  const panel = panelWidth(uiWidth);

  const pauseForSelection = () => {
    const { view: v, actions: a } = latest.current;
    dispatchControls({ type: "hide" });
    if (v.playing && !pausedByUs.current) {
      pausedByUs.current = true;
      a.pause();
    }
  };
  const resume = () => {
    if (!pausedByUs.current) return;
    pausedByUs.current = false;
    // Only resume what is still paused: the Learner may have pressed Play meanwhile.
    if (!latest.current.view.playing) latest.current.actions.toggle();
  };
  const discard = () => {
    setAnnounce("Selection too small, discarded.");
    resume();
  };

  const sel = useRegionSelection({
    onStart: pauseForSelection,
    onDiscard: discard,
  });
  const { selection, selectArea, adjust, clear } = sel;
  const selected = useLatest(selection);
  const voiceControls = useRef<VoiceBubbleControls>(null);
  const voiceNote = useVoiceQuickNote({
    surface: "immersive",
    controls: voiceControls,
    getPlayer: () => {
      const { view: v, actions: a } = latest.current;
      return {
        courseId: v.courseId,
        lessonId: v.lessonId,
        slideIndex: v.index,
        slideTitle: latest.current.slideTitle,
        timeMs: v.timeMs,
        playing: v.playing,
        region:
          selected.current.phase === "selected"
            ? selected.current.rect
            : undefined,
        pause: a.pause,
        play: a.toggle,
      };
    },
  });

  // A Slide change drops the selection: a region belongs to one Slide.
  const slideIndex = view.index;
  const lastSlide = useRef(slideIndex);
  useEffect(() => {
    if (lastSlide.current === slideIndex) return;
    lastSlide.current = slideIndex;
    clear();
    setComposer(null);
    setAnswer(null);
    pausedByUs.current = false;
    // `clear` is a stable reducer dispatch wrapper.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [slideIndex]);

  const openComposer = (rect?: RegionRect) => {
    pauseForSelection();
    setComposer({
      timeMs: Math.round(latest.current.view.timeMs),
      rect,
      slideIndex: latest.current.view.index,
    });
  };
  const closeComposer = () => {
    setComposer(null);
    clear();
    resume();
  };

  // Keep the selected region clear of the panel by moving the stage left.
  const shiftRect = answer?.rect;
  const shifting = answer !== null && docked;
  useEffect(() => {
    const root = document.documentElement;
    let shift = 0;
    if (shifting && shiftRect)
      shift = stageShift(shiftRect, uiWidth, layout.width, panel).shift;
    root.style.setProperty(
      "--imm-shift-x",
      layout.rotated ? "0px" : `${-shift}px`,
    );
    root.style.setProperty(
      "--imm-shift-y",
      layout.rotated ? `${-shift}px` : "0px",
    );
    root.style.setProperty("--imm-panel", `${panel}px`);
    // Captions never sit under the panel.
    root.style.setProperty(
      "--imm-panel-inset",
      shifting && !layout.rotated ? `${panel + 16}px` : "0px",
    );
    if (shifting) root.dataset.immPanel = "open";
    return () => {
      root.style.removeProperty("--imm-panel-inset");
      delete root.dataset.immPanel;
      root.style.removeProperty("--imm-shift-x");
      root.style.removeProperty("--imm-shift-y");
      root.style.removeProperty("--imm-panel");
    };
  }, [shifting, shiftRect, uiWidth, layout.width, layout.rotated, panel]);

  const openAnswer = (rect?: RegionRect) => {
    pauseForSelection();
    if (rect && selection.phase === "selected")
      trackRegion({
        event: "lesson_region_selected",
        course_id: latest.current.view.courseId,
        lesson_id: latest.current.view.lessonId,
        slide_index: latest.current.view.index,
        method: selection.method,
        area: rect.w * rect.h,
      });
    setAnswer({
      rect,
      atMs: Math.round(latest.current.view.timeMs),
      slideIndex: latest.current.view.index,
    });
  };
  const closeAnswer = (result: PanelOutcome) => {
    trackRegion({
      event: "lesson_answer_panel_closed",
      course_id: latest.current.view.courseId,
      lesson_id: latest.current.view.lessonId,
      slide_index: answer?.slideIndex ?? latest.current.view.index,
      outcome: result.outcome,
      saved_note: result.savedNote,
      follow_ups: result.followUps,
    });
    setAnswer(null);
    clear();
    resume();
  };

  const { t } = useUiPreferences();
  const chooseArea = () => {
    selectArea();
    setAutoFocusRegion(true);
    setAnnounce(
      "Select area. Arrow keys move the area, Shift and arrow keys resize it, Enter adds a note.",
    );
  };

  const rail = (
    <>
      <button
        type="button"
        className="imm-btn imm-icon-btn"
        data-testid="immersive-select-area"
        aria-label={t("imm.select_area")}
        title={t("imm.select_area")}
        onClick={chooseArea}
        disabled={composer !== null || answer !== null}
      >
        <Icon name="select" />
      </button>
      <button
        type="button"
        className="imm-btn imm-icon-btn"
        data-testid="immersive-ask"
        aria-label={t("imm.ask")}
        title={t("imm.ask")}
        onClick={() =>
          openAnswer(
            selection.phase === "selected" ? selection.rect : undefined,
          )
        }
        disabled={composer !== null || answer !== null}
      >
        <Icon name="ask" />
      </button>
      <div
        className="imm-voice-note"
        data-testid="immersive-voice-note"
        title={t("imm.voice_note")}
      >
        <VoiceBubble
          purpose="note"
          entryPoint="quick_note"
          lessonId={view.lessonId}
          icon="voicenote"
          testId="voice-note-mic"
          controls={voiceControls}
          disabled={composer !== null || answer !== null}
          {...voiceNote}
        />
      </div>
    </>
  );

  const extensions: ImmersiveExtensions = {
    sideRail: rail,
    onStageTap: () => {
      if (!answer) return false;
      closeAnswer({ outcome: "completed", savedNote: false, followUps: 0 });
      return true;
    },
    busy: composer !== null || answer !== null || selection.phase !== "none",
    ...sel.gestureExtensions,
  };

  const anchor: NoteAnchor | undefined = composer
    ? { playbackMs: composer.timeMs, rect: composer.rect, input: "typed" }
    : undefined;

  const uiStyle = layout.rotated
    ? {
        width: layout.vh,
        height: layout.vw,
        marginLeft: -layout.vh / 2,
        marginTop: -layout.vw / 2,
      }
    : undefined;

  return (
    <ImmersiveExtensionsContext.Provider value={extensions}>
      {children}
      <div className="imm-layer" data-testid="immersive-notes-layer">
        {highlight && selection.phase === "none" && (
          <StageSpace>
            <RegionOutline region={highlight} />
          </StageSpace>
        )}
        {answer?.rect && (
          <StageSpace>
            <RegionOutline region={answer.rect} />
          </StageSpace>
        )}
        {selection.phase !== "none" && answer === null && (
          <RegionSelector
            region={selection.rect}
            drawing={selection.phase === "drawing"}
            autoFocus={autoFocusRegion}
            onChange={(r, method) => adjust(r, method)}
            onConfirm={(r) => openComposer(r)}
            onCancel={() => {
              clear();
              resume();
              setAutoFocusRegion(false);
            }}
            actions={
              composer ? null : (
                <>
                  <button
                    type="button"
                    className="imm-btn imm-btn-big"
                    data-testid="region-ask"
                    onClick={() => openAnswer(selection.rect)}
                  >
                    Ask
                  </button>
                  <button
                    type="button"
                    className="imm-btn"
                    data-testid="region-note"
                    onClick={() => openComposer(selection.rect)}
                  >
                    Add note
                  </button>
                  <button
                    type="button"
                    className="imm-btn"
                    data-testid="region-clear"
                    onClick={() => {
                      clear();
                      resume();
                    }}
                  >
                    Clear
                  </button>
                </>
              )
            }
          />
        )}
      </div>
      {answer && docked && (
        <div
          className="imm-layer imm-ui imm-answer-ui"
          data-rotated={layout.rotated || undefined}
          style={uiStyle}
        >
          <AnswerPanel
            courseId={view.courseId}
            lessonId={view.lessonId}
            slideIndex={answer.slideIndex}
            slideTitle={slideTitle}
            region={answer.rect}
            atMs={answer.atMs}
            rotated={layout.rotated}
            onRestore={(rect) => setAnswer((a) => (a ? { ...a, rect } : a))}
            onClose={closeAnswer}
          />
        </div>
      )}
      {answer && !docked && (
        <div className="imm-layer imm-ui imm-notes">
          <ResponsivePanel
            title="Ask tutor"
            open={true}
            onClose={() =>
              closeAnswer({
                outcome: "completed",
                savedNote: false,
                followUps: 0,
              })
            }
          >
            <TutorChat
              courseId={view.courseId}
              lesson={{
                lesson_id: view.lessonId,
                slide_index: answer.slideIndex,
              }}
              region={
                answer.rect
                  ? { rect: answer.rect, at_ms: answer.atMs, input: "text" }
                  : undefined
              }
              embedded
              onClose={() =>
                closeAnswer({
                  outcome: "completed",
                  savedNote: false,
                  followUps: 0,
                })
              }
            />
          </ResponsivePanel>
        </div>
      )}
      {composer && (
        <div
          className="imm-layer imm-ui imm-notes"
          data-rotated={layout.rotated || undefined}
          style={uiStyle}
          data-testid="immersive-note-composer"
        >
          <QuickNotePanel
            open={true}
            voiceVariant="bubble"
            onClose={closeComposer}
            context={{
              source: "slide",
              courseId: view.courseId,
              lessonId: view.lessonId,
              slideId: composer.slideIndex,
              slideTitle,
              anchor,
            }}
          />
        </div>
      )}
      <ToastHost />
      <p
        className="imm-sr"
        role="status"
        data-testid="immersive-notes-announcer"
      >
        {announce}
      </p>
    </ImmersiveExtensionsContext.Provider>
  );
}
