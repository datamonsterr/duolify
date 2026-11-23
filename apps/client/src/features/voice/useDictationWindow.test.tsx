import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const track = vi.hoisted(() => vi.fn());
vi.mock("../../lib/analytics", () => ({ trackVoice: track }));
import { useDictationWindow } from "./useDictationWindow";

function setup(draft = "") {
  const send = vi.fn();
  const view = renderHook(
    ({ draft }: { draft: string }) =>
      useDictationWindow({ entryPoint: "answer_panel", draft, send }),
    { initialProps: { draft } },
  );
  return { ...view, send };
}
const events = () => track.mock.calls.map((c) => [c[0].event, c[0].edited]);

beforeEach(() => {
  vi.useFakeTimers();
  localStorage.clear();
  track.mockClear();
});
afterEach(() => vi.useRealTimers());

describe("dictation edit window", () => {
  it("sends automatically after 2 seconds", () => {
    const { result, rerender, send } = setup();
    rerender({ draft: "what is a closure" });
    act(() => result.current.begin("what is a closure", "en"));
    expect(result.current.counting).toBe(true);
    act(() => void vi.advanceTimersByTime(1999));
    expect(send).not.toHaveBeenCalled();
    act(() => void vi.advanceTimersByTime(2));
    expect(send).toHaveBeenCalledTimes(1);
    expect(events()).toEqual([["voice_transcript_used", false]]);
  });
  it("an edit within the window stops the automatic send; sending later is used with edited", () => {
    const { result, rerender, send } = setup();
    rerender({ draft: "what is a closure" });
    act(() => result.current.begin("what is a closure", "en"));
    act(() => void vi.advanceTimersByTime(1000));
    rerender({ draft: "what is a closure?" });
    expect(result.current.counting).toBe(false);
    act(() => void vi.advanceTimersByTime(5000));
    expect(send).not.toHaveBeenCalled();
    act(() => result.current.noteSent());
    expect(events()).toEqual([["voice_transcript_used", true]]);
  });
  it("Cancel stops the countdown but keeps the text", () => {
    const { result, rerender, send } = setup();
    rerender({ draft: "hello" });
    act(() => result.current.begin("hello", "en"));
    act(() => result.current.cancelAuto());
    act(() => void vi.advanceTimersByTime(5000));
    expect(send).not.toHaveBeenCalled();
    expect(events()).toEqual([]);
  });
  it("clearing the field discards the transcript", () => {
    const { result, rerender } = setup();
    rerender({ draft: "hello" });
    act(() => result.current.begin("hello", "en"));
    rerender({ draft: "" });
    expect(events()).toEqual([["voice_transcript_discarded", true]]);
  });
  it("does not auto-send Vietnamese by default, but the Learner's choice wins", () => {
    const vi1 = setup();
    vi1.rerender({ draft: "xin chao" });
    act(() => vi1.result.current.begin("xin chao", "vi"));
    expect(vi1.result.current.counting).toBe(false);
    localStorage.setItem("duolify.voice.auto-send", "on");
    const vi2 = setup();
    vi2.rerender({ draft: "xin chao" });
    act(() => vi2.result.current.begin("xin chao", "vi"));
    expect(vi2.result.current.counting).toBe(true);
  });
  it("respects the explicit-send setting", () => {
    localStorage.setItem("duolify.voice.auto-send", "off");
    const { result, rerender, send } = setup();
    rerender({ draft: "hello" });
    act(() => result.current.begin("hello", "en"));
    act(() => void vi.advanceTimersByTime(10_000));
    expect(send).not.toHaveBeenCalled();
  });
});
