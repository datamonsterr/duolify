import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({ request: vi.fn() }));
vi.mock("../accounts/session", () => ({
  session: { request: mocks.request },
}));
vi.mock("../../lib/analytics", () => ({
  trackGuidedUi: vi.fn(),
  trackVoice: vi.fn(),
}));
vi.mock("../voice/VoiceButton", () => ({
  VoiceButton: ({
    onTranscript,
  }: {
    onTranscript: (t: { text: string; language: string }) => void;
  }) => (
    <button
      type="button"
      data-testid="fake-mic"
      onClick={() => onTranscript({ text: "spoken words", language: "en" })}
    >
      mic
    </button>
  ),
}));
import { QuickNotePanel, clearQuickNoteDrafts } from "./QuickNotePanel";
import { noteQueue } from "./note-queue";

const base = {
  source: "slide" as const,
  courseId: "c1",
  lessonId: "l1",
  slideId: 3,
  slideTitle: "Sky",
};

beforeEach(() => {
  localStorage.clear();
  clearQuickNoteDrafts();
  noteQueue.clear();
  mocks.request.mockReset();
});

describe("Quick note guards", () => {
  it("a new anchored moment starts a fresh note even when an older one is unsynced", () => {
    noteQueue.enqueue({
      ...base,
      anchor: { playbackMs: 1000, input: "typed" },
      rawText: "older",
      status: "device_saved",
    });
    render(
      <QuickNotePanel
        open
        onClose={vi.fn()}
        context={{
          ...base,
          anchor: { playbackMs: 9000, input: "typed" },
        }}
        isOffline
      />,
    );
    expect(screen.getByLabelText(/your note/i)).toBeTruthy();
  });

  it("dictating after typing keeps the typed text and appends the transcript", () => {
    const onSync = vi.fn();
    render(
      <QuickNotePanel
        open
        onClose={vi.fn()}
        context={{ ...base, anchor: { playbackMs: 1, input: "typed" } }}
        onSync={onSync}
      />,
    );
    const field = screen.getByLabelText(/your note/i) as HTMLTextAreaElement;
    fireEvent.change(field, { target: { value: "half a thought" } });
    fireEvent.click(screen.getByTestId("fake-mic"));
    expect(onSync).not.toHaveBeenCalled();
    expect(noteQueue.getQueue()).toHaveLength(0);
    expect(
      (screen.getByLabelText(/your note/i) as HTMLTextAreaElement).value,
    ).toBe("half a thought spoken words");
  });

  it("a double tap on Save queues one note", async () => {
    let release!: (v: { id: string; status: string }) => void;
    const onSync = vi.fn(
      () => new Promise<{ id: string; status: string }>((r) => (release = r)),
    );
    render(
      <QuickNotePanel
        open
        onClose={vi.fn()}
        context={{ ...base, anchor: { playbackMs: 1, input: "typed" } }}
        onSync={onSync}
      />,
    );
    fireEvent.change(screen.getByLabelText(/your note/i), {
      target: { value: "once" },
    });
    const save = screen.getByText("Save quick note");
    fireEvent.click(save);
    fireEvent.click(save);
    expect(onSync).toHaveBeenCalledTimes(1);
    expect(noteQueue.getQueue()).toHaveLength(1);
    release({ id: "s1", status: "saved" });
    await waitFor(() => expect(screen.queryByText("Syncing…")).toBeNull());
  });
});
