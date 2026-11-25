import { act, fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

vi.mock("../../notes/QuickNotePanel", () => ({
  QuickNotePanel: ({ onClose }: { onClose: () => void }) => (
    <button type="button" data-testid="close-composer" onClick={onClose}>
      close
    </button>
  ),
}));
vi.mock("../../chat/TutorChat", () => ({ TutorChat: () => null }));
vi.mock("./AnswerPanel", () => ({ default: () => null }));
vi.mock("../../../lib/analytics", async (orig) => ({
  ...(await orig<object>()),
  trackRegion: vi.fn(),
}));
import ImmersiveNotes from "./ImmersiveNotes";
import { useImmersiveExtensions } from "./extensions";
import type { ImmersiveActions, ImmersiveView } from "./types";

const base = {
  title: "T",
  lessonId: "l",
  courseId: "c",
  index: 0,
  slides: [{ id: "a", label: "A", durationMs: 1000 }],
  timeMs: 0,
  durationMs: 1000,
} as unknown as ImmersiveView;
// The panel is reached by choosing an area and adding a note (the rail Note button is now Voice note).
const openNote = () => {
  fireEvent.click(screen.getByTestId("immersive-select-area"));
  fireEvent.click(screen.getByTestId("region-note"));
};
const Rail = () => <div>{useImmersiveExtensions().sideRail}</div>;

describe("closing a note after the Learner pressed Play", () => {
  it("does not toggle a playing slide back to paused", () => {
    const actions = {
      pause: vi.fn(),
      toggle: vi.fn(),
    } as unknown as ImmersiveActions;
    const ui = (playing: boolean) => (
      <ImmersiveNotes
        view={{ ...base, playing }}
        actions={actions}
        slideTitle="A"
      >
        <Rail />
      </ImmersiveNotes>
    );
    const { rerender } = render(ui(true));
    openNote();
    expect(actions.pause).toHaveBeenCalledTimes(1);
    rerender(ui(true)); // the Learner pressed Play while the note was open
    act(() => void fireEvent.click(screen.getByTestId("close-composer")));
    expect(actions.toggle).not.toHaveBeenCalled();
  });
  it("resumes a slide that is still paused", () => {
    const actions = {
      pause: vi.fn(),
      toggle: vi.fn(),
    } as unknown as ImmersiveActions;
    const ui = (playing: boolean) => (
      <ImmersiveNotes
        view={{ ...base, playing }}
        actions={actions}
        slideTitle="A"
      >
        <Rail />
      </ImmersiveNotes>
    );
    const { rerender } = render(ui(true));
    openNote();
    rerender(ui(false)); // our pause took effect
    fireEvent.click(screen.getByTestId("close-composer"));
    expect(actions.toggle).toHaveBeenCalledTimes(1);
  });
});
