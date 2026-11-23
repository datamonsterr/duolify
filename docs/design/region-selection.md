# Region selection component (immersive lesson player)

Reusable overlay for choosing a rectangle on the Slide. First used by anchored Quick notes (group 5); the tutor "ask about this area" panel (group 4) reuses it unchanged.
All code is in `apps/client/src/features/lesson-player/immersive/` and loads with the lazy immersive chunk (except `RegionOutline`, which is tiny and also used in the framed player).

## Coordinates

Every rectangle is `RegionRect = { x, y, w, h }`, normalized 0..1 on the 16:9 stage, with rotation and letterboxing already undone. A region under 6% of the stage in either direction (`MIN_REGION`) is an accidental touch and is discarded.

## Pieces

| Piece | Role |
|---|---|
| `useRegionSelection({ onStart, onDiscard })` | State + wiring. Returns `{ selection, gestureExtensions, selectArea, adjust, clear }`. `selection` is `{phase:"none"}`, `{phase:"drawing", rect}` (fingers still down) or `{phase:"selected", rect, method}` with `method` of `two_finger`, `select_area` or `keyboard` (the analytics `method` enum). `onStart` fires when a spread begins or Select area is chosen (pause playback there); `onDiscard` when a spread was accidental (resume there). |
| `<RegionSelector>` | Controlled overlay. Dimmed surround, outline, four corner handles, drag to move. Mount it only while `selection.phase !== "none"`. |
| `<RegionOutline region>` | Static outline (no events), for "show in Lesson" and for echoing a region. |
| `<StageSpace>` | A 1280×720 box laid over the stage with the same scale and rotation. Build anything else stage-anchored inside it. |
| `region.ts` | Pure maths: `defaultRegion`, `clampRegion`, `moveRegion`, `resizeRegion`, `keyboardAdjust`, `selectionReducer`. |

## RegionSelector props

```ts
{
  region: RegionRect;                     // current rectangle (controlled)
  drawing?: boolean;                      // spread in progress: no handles, no events
  onChange(region, method): void;         // drag, handle or keyboard; store and pass back
  onConfirm?(region): void;               // Enter on the region
  onCancel?(): void;                      // Escape on the region
  actions?: ReactNode;                    // buttons shown beside the region (Add note, Ask ...)
  label?: string;                         // accessible name
  autoFocus?: boolean;                    // focus the region (keyboard "Select area")
}
```

## Wiring

```tsx
const sel = useRegionSelection({ onStart: pause, onDiscard: resume });
<ImmersiveExtensionsContext.Provider value={{ sideRail, ...sel.gestureExtensions }}>
  <ImmersiveLayer .../>
  {sel.selection.phase !== "none" && (
    <RegionSelector
      region={sel.selection.rect}
      drawing={sel.selection.phase === "drawing"}
      onChange={(r, m) => sel.adjust(r, m)}
      onCancel={sel.clear}
      actions={<button onClick={...}>Add note</button>}
    />
  )}
</ImmersiveExtensionsContext.Provider>
```

`ImmersiveNotes.tsx` is the reference implementation. A rail button calls `sel.selectArea()` (places a centred 40% box); the two-finger spread arrives through `gestureExtensions`.

## Accessibility

The region is a focusable group: arrows move by 1% (Alt: 5%), Shift+arrows resize the bottom-right corner, Enter confirms, Escape cancels. Each handle is a button (44 px, scaled against the stage) that resizes with the arrows. Playback pauses when selection begins.

## Answer panel (group 4)

`AnswerPanel.tsx` is the opaque panel docked to the right of the immersive stage (`panelLayout.ts`: about 40% of the width, 320 to 480 px). It runs `useTutor(courseId, tutorService, true, lesson, region)`: the same region goes on the quote and the message, and the reply is the existing SSE stream. While it is open the stage is moved left (`--imm-shift-x/y`, CSS `translate`) just enough to keep the whole region clear of the panel; captions stay left of it. Close resumes playback only if the panel paused it. Screens too narrow to dock (UI width under 600 px, unrotated) use the lesson tutor sheet (`TutorChat embedded`) with a thumbnail of the area instead. Thread reads return `anchor` per message, used to offer re-outlining an earlier area.
