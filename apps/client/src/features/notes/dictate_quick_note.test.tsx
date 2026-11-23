import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({ request: vi.fn(), track: vi.fn() }));
vi.mock("../accounts/session", () => ({
  session: { request: mocks.request },
}));
vi.mock("../../lib/analytics", () => ({
  trackGuidedUi: vi.fn(),
  trackVoice: mocks.track,
}));
// The microphone itself is covered in features/voice; here a transcript simply arrives.
vi.mock("../voice/VoiceButton", () => ({
  VoiceButton: ({
    onTranscript,
  }: {
    onTranscript: (t: { text: string; language: string }) => void;
  }) => (
    <button
      type="button"
      data-testid="fake-mic"
      onClick={() =>
        onTranscript({ text: "stars move because earth spins", language: "en" })
      }
    >
      mic
    </button>
  ),
}));
import { QuickNotePanel, clearQuickNoteDrafts } from "./QuickNotePanel";
import { noteQueue } from "./note-queue";

const context = {
  source: "slide" as const,
  courseId: "c1",
  lessonId: "l1",
  slideId: 2,
  slideTitle: "Sky",
  anchor: { playbackMs: 4200, input: "typed" as const },
};

beforeEach(() => {
  localStorage.clear();
  clearQuickNoteDrafts();
  noteQueue.clear();
  mocks.request.mockReset();
  mocks.track.mockClear();
});

describe("dictated Quick note", () => {
  it("saves at once as a raw note with a voice anchor, then Undo removes it everywhere", async () => {
    const sync = vi.fn(async () => ({ id: "srv-1", status: "saved" }));
    mocks.request.mockResolvedValue(undefined);
    render(
      <QuickNotePanel open onClose={vi.fn()} context={context} onSync={sync} />,
    );
    fireEvent.click(screen.getByTestId("fake-mic"));
    await waitFor(() => expect(sync).toHaveBeenCalledTimes(1));
    expect(sync).toHaveBeenCalledWith(
      expect.objectContaining({
        rawText: "stars move because earth spins",
        anchor: { playbackMs: 4200, input: "voice" },
      }),
    );
    expect(noteQueue.getQueue()).toHaveLength(1);
    expect(mocks.track).toHaveBeenCalledWith(
      expect.objectContaining({
        event: "voice_transcript_used",
        purpose: "note",
        entry_point: "quick_note",
      }),
    );
    // No transcript text in any analytics payload.
    expect(JSON.stringify(mocks.track.mock.calls)).not.toContain("stars move");

    fireEvent.click(await screen.findByTestId("voice-note-undo"));
    await waitFor(() =>
      expect(mocks.request).toHaveBeenCalledWith("/v1/notes/srv-1", "DELETE"),
    );
    expect(noteQueue.getQueue()).toHaveLength(0);
    expect(screen.queryByTestId("voice-note-saved")).toBeNull();
    expect(mocks.track).toHaveBeenCalledWith(
      expect.objectContaining({ event: "voice_transcript_discarded" }),
    );
  });

  it("offline, the dictated note is kept on the device and Undo drops it from the queue", async () => {
    const sync = vi.fn();
    render(
      <QuickNotePanel
        open
        onClose={vi.fn()}
        context={context}
        onSync={sync}
        isOffline
      />,
    );
    fireEvent.click(screen.getByTestId("fake-mic"));
    await waitFor(() => expect(noteQueue.getQueue()).toHaveLength(1));
    expect(sync).not.toHaveBeenCalled();
    fireEvent.click(await screen.findByTestId("voice-note-undo"));
    await waitFor(() => expect(noteQueue.getQueue()).toHaveLength(0));
    expect(mocks.request).not.toHaveBeenCalled();
  });
});
