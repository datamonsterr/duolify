import { act, renderHook } from "@testing-library/react";
import { createRef } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const saveNote = vi.fn();
const undoSavedNote = vi.fn();
const showToast = vi.fn();
const track = vi.fn();
vi.mock("./save-note", () => ({
  saveNote: (...a: unknown[]) => saveNote(...a),
  undoSavedNote: (...a: unknown[]) => undoSavedNote(...a),
}));
vi.mock("../../components/toast", () => ({
  showToast: (...a: unknown[]) => showToast(...a),
}));
vi.mock("../../lib/analytics", () => ({
  trackVoiceQuickNote: (...a: unknown[]) => track(...a),
}));

import {
  startVoiceQuickNote,
  stopVoiceQuickNote,
  useVoiceQuickNote,
  type QuickNotePlayer,
} from "./useVoiceQuickNote";

function setup(over: Partial<QuickNotePlayer> = {}) {
  const player: QuickNotePlayer = {
    courseId: "c1",
    lessonId: "l1",
    slideIndex: 2,
    slideTitle: "Why the sky is blue",
    timeMs: 4321.6,
    playing: true,
    pause: vi.fn(() => void (player.playing = false)),
    play: vi.fn(() => void (player.playing = true)),
    ...over,
  };
  const controls = createRef<{ press: () => void; release: () => void }>();
  const hook = renderHook(() =>
    useVoiceQuickNote({
      surface: "immersive",
      controls,
      getPlayer: () => player,
    }),
  );
  return { player, controls, hook };
}

beforeEach(() => {
  vi.clearAllMocks();
  saveNote.mockResolvedValue({ actionKey: "k1", id: "n1", synced: true });
});

describe("useVoiceQuickNote", () => {
  it("pauses a playing Lesson on press and resumes it on release", () => {
    const { player, hook } = setup();
    act(() => hook.result.current.onRecording(true));
    expect(player.pause).toHaveBeenCalledOnce();
    act(() => hook.result.current.onRecording(false));
    expect(player.play).toHaveBeenCalledOnce();
  });

  it("does not resume when the Learner pressed Play meanwhile", () => {
    const { player, hook } = setup();
    act(() => hook.result.current.onRecording(true));
    player.playing = true;
    act(() => hook.result.current.onRecording(false));
    expect(player.play).not.toHaveBeenCalled();
  });

  it("does not touch a Lesson that was already paused", () => {
    const { player, hook } = setup({ playing: false });
    act(() => hook.result.current.onRecording(true));
    act(() => hook.result.current.onRecording(false));
    expect(player.pause).not.toHaveBeenCalled();
    expect(player.play).not.toHaveBeenCalled();
  });

  it("saves the transcript anchored to the press moment as a voice note, then offers Undo", async () => {
    const { player, hook } = setup({
      region: { x: 0.1, y: 0.2, w: 0.3, h: 0.4 },
    });
    act(() => hook.result.current.onRecording(true));
    player.timeMs = 9999;
    player.slideIndex = 7;
    await act(() =>
      hook.result.current.onTranscript({
        text: " Orange sky ",
        language: "en",
      }),
    );
    const call = saveNote.mock.calls[0][0];
    expect(call.body).toBe("Orange sky");
    expect(call.voice).toBe(true);
    expect(call.context).toMatchObject({
      source: "slide",
      courseId: "c1",
      lessonId: "l1",
      slideId: 2,
      slideTitle: "Why the sky is blue",
      anchor: {
        playbackMs: 4322,
        rect: { x: 0.1, y: 0.2, w: 0.3, h: 0.4 },
        input: "voice",
      },
    });
    const toast = showToast.mock.calls[0][0];
    expect(toast.action.label).toBeTruthy();
    toast.action.onAction();
    expect(undoSavedNote).toHaveBeenCalledWith({
      actionKey: "k1",
      id: "n1",
      synced: true,
    });
    expect(track.mock.calls.map((c) => c[0].event)).toEqual([
      "voice_quick_note_started",
      "voice_quick_note_saved",
      "voice_quick_note_undone",
    ]);
  });

  it("a cancelled recording saves nothing", async () => {
    const { hook } = setup();
    act(() => hook.result.current.onRecording(true));
    act(() => hook.result.current.onCancelled());
    await act(() =>
      hook.result.current.onTranscript({ text: "late", language: "en" }),
    );
    expect(saveNote).not.toHaveBeenCalled();
    expect(track.mock.calls[track.mock.calls.length - 1]?.[0].event).toBe(
      "voice_quick_note_cancelled",
    );
  });

  it("analytics payloads carry no text, title or ids", async () => {
    const { hook } = setup();
    act(() => hook.result.current.onRecording(true));
    await act(() =>
      hook.result.current.onTranscript({
        text: "secret words",
        language: "en",
      }),
    );
    act(() => hook.result.current.onNotice({ kind: "empty", retry: true }));
    const blob = JSON.stringify(track.mock.calls);
    expect(blob).not.toContain("secret");
    expect(blob).not.toContain("sky");
    expect(blob).not.toContain("l1");
    expect(track.mock.calls[track.mock.calls.length - 1]?.[0]).toMatchObject({
      event: "voice_quick_note_failed",
      reason: "empty",
    });
  });

  it("exports start and stop actions that drive the mounted control", () => {
    const press = vi.fn();
    const release = vi.fn();
    const { controls } = setup();
    expect(startVoiceQuickNote()).toBe(false);
    (controls as { current: unknown }).current = { press, release };
    // Mounted hook registered its controls on render.
    const again = renderHook(() =>
      useVoiceQuickNote({
        surface: "player",
        controls,
        getPlayer: () => setup().player,
      }),
    );
    expect(startVoiceQuickNote()).toBe(true);
    expect(stopVoiceQuickNote()).toBe(true);
    expect(press).toHaveBeenCalledOnce();
    expect(release).toHaveBeenCalledOnce();
    again.unmount();
    expect(startVoiceQuickNote()).toBe(false);
  });
});
