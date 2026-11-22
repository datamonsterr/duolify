import { useEffect, useRef, useState, type ReactNode } from "react";
import { QuickNotePanel } from "../../notes/QuickNotePanel";
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
  const latest = useLatest({ view, actions });
  // Playback this component paused, so closing resumes only what it stopped.
  const pausedByUs = useRef(false);
  const [announce, setAnnounce] = useState("");
  const [composer, setComposer] = useState<{
    timeMs: number;
    rect?: RegionRect;
    slideIndex: number;
  } | null>(null);
  const [autoFocusRegion, setAutoFocusRegion] = useState(false);

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
    latest.current.actions.toggle();
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

  // A Slide change drops the selection: a region belongs to one Slide.
  const slideIndex = view.index;
  const lastSlide = useRef(slideIndex);
  useEffect(() => {
    if (lastSlide.current === slideIndex) return;
    lastSlide.current = slideIndex;
    clear();
    setComposer(null);
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
        className="imm-btn"
        data-testid="immersive-select-area"
        onClick={chooseArea}
        disabled={composer !== null}
      >
        Select area
      </button>
      <button
        type="button"
        className="imm-btn"
        data-testid="immersive-quick-note"
        onClick={() =>
          openComposer(
            selection.phase === "selected" ? selection.rect : undefined,
          )
        }
        disabled={composer !== null}
      >
        Quick note
      </button>
    </>
  );

  const extensions: ImmersiveExtensions = {
    sideRail: rail,
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
        {selection.phase !== "none" && (
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
      {composer && (
        <div
          className="imm-layer imm-ui imm-notes"
          data-rotated={layout.rotated || undefined}
          style={uiStyle}
          data-testid="immersive-note-composer"
        >
          <QuickNotePanel
            open={true}
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
