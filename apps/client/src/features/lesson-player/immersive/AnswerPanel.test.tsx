import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const m = vi.hoisted(() => ({
  tutor: {} as Record<string, unknown>,
  send: vi.fn(),
  track: vi.fn(),
}));
vi.mock("../../accounts/session", () => ({ session: { request: vi.fn() } }));
vi.mock("../../../lib/analytics", () => ({
  trackVoice: m.track,
  trackGuidedUi: vi.fn(),
}));
vi.mock("../../chat/useTutor", () => ({ useTutor: () => m.tutor }));
vi.mock("../../voice/VoiceButton", () => ({
  VoiceButton: ({
    onTranscript,
  }: {
    onTranscript: (t: { text: string; language: string }) => void;
  }) => (
    <button
      type="button"
      data-testid="fake-mic"
      onClick={() => onTranscript({ text: "why is it blue", language: "en" })}
    >
      mic
    </button>
  ),
}));
import AnswerPanel from "./AnswerPanel";

function tutor(over: Record<string, unknown> = {}) {
  m.tutor = {
    draft: "why is it blue",
    setDraft: vi.fn(),
    messages: [],
    loading: false,
    phase: "idle",
    online: true,
    block: undefined,
    notice: undefined,
    send: m.send,
    stop: vi.fn(),
    ...over,
  };
}
const panel = () => (
  <AnswerPanel
    courseId="c"
    lessonId="l"
    slideIndex={0}
    slideTitle="Sky"
    atMs={0}
    onRestore={vi.fn()}
    onClose={vi.fn()}
  />
);

beforeEach(() => {
  vi.useFakeTimers();
  localStorage.clear();
  m.send.mockClear();
  m.track.mockClear();
  tutor();
});
afterEach(() => vi.useRealTimers());

describe("immersive answer panel", () => {
  it("Enter inside an IME composition does not send", () => {
    render(panel());
    const field = screen.getByRole("textbox");
    fireEvent.keyDown(field, { key: "Enter", isComposing: true });
    expect(m.send).not.toHaveBeenCalled();
    fireEvent.keyDown(field, { key: "Enter" });
    expect(m.send).toHaveBeenCalledTimes(1);
  });

  it("a dictated question is sent after the edit window", () => {
    render(panel());
    fireEvent.click(screen.getByTestId("fake-mic"));
    act(() => void vi.advanceTimersByTime(2100));
    expect(m.send).toHaveBeenCalledTimes(1);
  });

  it("a dictated question never auto-sends while the quote needs approval, and is not counted as used", () => {
    tutor({ block: { kind: "approval" } });
    render(panel());
    fireEvent.click(screen.getByTestId("fake-mic"));
    act(() => void vi.advanceTimersByTime(5000));
    expect(m.send).not.toHaveBeenCalled();
    expect(
      m.track.mock.calls.filter((c) => c[0].event === "voice_transcript_used"),
    ).toEqual([]);
  });
});
