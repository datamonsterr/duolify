import { act, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

vi.mock("../../notes/format-turns", async (orig) => ({
  ...(await orig<object>()),
  refreshFormatTurns: vi.fn(),
}));
vi.mock("../../chat/TutorChat", () => ({ TutorChat: () => null }));
vi.mock("./AnswerPanel", () => ({ default: () => null }));
import { setFormatTurnsLeft } from "../../notes/format-turns";
import ImmersiveNotes from "./ImmersiveNotes";
import { useImmersiveExtensions } from "./extensions";
import type { ImmersiveActions, ImmersiveView } from "./types";

const view = {
  title: "T",
  lessonId: "l",
  courseId: "c",
  index: 0,
  slides: [{ id: "a", label: "A", durationMs: 1000 }],
  timeMs: 0,
  durationMs: 1000,
  playing: false,
} as unknown as ImmersiveView;
const Rail = () => <div>{useImmersiveExtensions().sideRail}</div>;
const mount = () =>
  render(
    <ImmersiveNotes view={view} actions={{} as ImmersiveActions} slideTitle="A">
      <Rail />
    </ImmersiveNotes>,
  );

describe("Voice note free-format badge", () => {
  it("shows the free formats left with an accessible label, and hides at zero or unknown", () => {
    act(() => setFormatTurnsLeft(null));
    mount();
    expect(screen.queryByTestId("voice-note-free-left")).toBeNull();
    act(() => setFormatTurnsLeft(5));
    const badge = screen.getByTestId("voice-note-free-left");
    expect(badge.textContent).toBe("5");
    expect(badge.getAttribute("aria-label")).toBe("5 free formats left");
    act(() => setFormatTurnsLeft(1));
    expect(
      screen.getByTestId("voice-note-free-left").getAttribute("aria-label"),
    ).toBe("1 free format left");
    act(() => setFormatTurnsLeft(0));
    expect(screen.queryByTestId("voice-note-free-left")).toBeNull();
  });
});
