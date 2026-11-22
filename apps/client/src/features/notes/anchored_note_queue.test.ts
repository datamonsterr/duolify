import { beforeEach, describe, expect, it } from "vitest";
import { anchorToWire, noteQueue } from "./note-queue";

const anchor = {
  playbackMs: 12_345.6,
  rect: { x: 0.1, y: 0.2, w: 0.4, h: 0.3 },
  input: "typed" as const,
};

describe("anchor in the offline note queue", () => {
  beforeEach(() => localStorage.clear());

  it("survives a reload: the queue is serialised with the anchor", () => {
    const queued = noteQueue.enqueue({
      lessonId: "l1",
      slideId: 2,
      source: "slide",
      rawText: "why orange?",
      anchor,
    });
    const reread = noteQueue
      .getQueue()
      .find((n) => n.actionKey === queued.actionKey);
    expect(reread?.anchor).toEqual(anchor);
  });

  it("replays with the wire shape the API expects, time rounded to whole ms", () => {
    expect(anchorToWire(anchor)).toEqual({
      playback_ms: 12_346,
      rect: anchor.rect,
      input: "typed",
    });
    expect(anchorToWire({ playbackMs: -5, input: "voice" })).toEqual({
      playback_ms: 0,
      rect: undefined,
      input: "voice",
    });
    expect(anchorToWire(undefined)).toBeUndefined();
  });

  it("an update keeps the anchor and a note without one has none", () => {
    const withAnchor = noteQueue.enqueue({
      source: "slide",
      slideId: 1,
      rawText: "a",
      anchor,
    });
    noteQueue.markSynced(withAnchor.actionKey, "server-1");
    expect(noteQueue.getQueue()[0].anchor).toEqual(anchor);
    const plain = noteQueue.enqueue({ source: "lesson", rawText: "b" });
    expect(plain.anchor).toBeUndefined();
  });
});
