import { describe, expect, it, beforeEach, vi, afterEach } from "vitest";
import * as analyticsModule from "../../lib/analytics";
import {
  isChatEdgeEnabled,
  calculateBackoff,
  FrameDeduplicator,
  parseWsMessage,
  formatWsCommand,
  createPrivacySafeError,
  notifyEdgeFallback,
  sendGuidedTurn,
} from "./chat-transport";

describe("CF-06 chat transport unit tests", () => {
  beforeEach(() => {
    delete (window as unknown as Record<string, unknown>).__CHAT_EDGE_ENABLED__;
  });

  afterEach(() => {
    vi.restoreAllMocks();
    delete (window as unknown as Record<string, unknown>).__CHAT_EDGE_ENABLED__;
  });

  describe("flag gating", () => {
    it("defaults to false (off) when unconfigured", () => {
      expect(isChatEdgeEnabled()).toBe(false);
    });

    it("honors window override", () => {
      (window as unknown as Record<string, unknown>).__CHAT_EDGE_ENABLED__ =
        true;
      expect(isChatEdgeEnabled()).toBe(true);

      (window as unknown as Record<string, unknown>).__CHAT_EDGE_ENABLED__ =
        false;
      expect(isChatEdgeEnabled()).toBe(false);
    });
  });

  describe("event parsing & wire format", () => {
    it("formats command to JSON wire with id, op and payload", () => {
      const cmd = formatWsCommand("req-1", "quote", {
        class: "short",
        message: "Hi",
      });
      const parsed = JSON.parse(cmd);
      expect(parsed).toEqual({
        id: "req-1",
        op: "quote",
        body: { class: "short", message: "Hi" },
      });
    });

    it("formats stop command with key", () => {
      const cmd = formatWsCommand("req-2", "stop", undefined, "turn-key-1");
      const parsed = JSON.parse(cmd);
      expect(parsed).toEqual({
        id: "req-2",
        op: "stop",
        key: "turn-key-1",
      });
    });

    it("parses unary result frame", () => {
      const raw = JSON.stringify({
        id: "req-1",
        event: "result",
        status: 200,
        data: { id: "quote-1", content_ceiling: 5 },
      });
      const parsed = parseWsMessage(raw);
      expect(parsed).toEqual({
        type: "result",
        id: "req-1",
        status: 200,
        data: { id: "quote-1", content_ceiling: 5 },
      });
    });

    it("parses heartbeat frame", () => {
      const raw = JSON.stringify({
        id: "req-1",
        event: "heartbeat",
        data: null,
      });
      const parsed = parseWsMessage(raw);
      expect(parsed).toEqual({
        type: "heartbeat",
        id: "req-1",
      });
    });

    it("parses stream frames (thread, delta, done, error)", () => {
      const deltaRaw = JSON.stringify({
        id: "req-1",
        event: "delta",
        data: { text: "Hello world" },
        seq: 2,
      });
      expect(parseWsMessage(deltaRaw)).toEqual({
        type: "stream",
        id: "req-1",
        event: "delta",
        data: { text: "Hello world" },
        seq: 2,
      });

      const doneRaw = JSON.stringify({
        id: "req-1",
        event: "done",
        data: { message_id: "m1", thread_id: "t1", replayed: false },
        seq: 3,
      });
      expect(parseWsMessage(doneRaw)).toEqual({
        type: "stream",
        id: "req-1",
        event: "done",
        data: { message_id: "m1", thread_id: "t1", replayed: false },
        seq: 3,
      });
    });
  });

  describe("frame sequencing & deduplication", () => {
    it("deduplicates frames with identical sequence numbers", () => {
      const dedup = new FrameDeduplicator();
      expect(dedup.accept(1)).toBe(true);
      expect(dedup.accept(1)).toBe(false); // duplicate
      expect(dedup.accept(2)).toBe(true);
      expect(dedup.accept(2)).toBe(false); // duplicate
      expect(dedup.accept(3)).toBe(true);
    });

    it("rejects out-of-order or duplicate past sequence numbers", () => {
      const dedup = new FrameDeduplicator();
      expect(dedup.accept(1)).toBe(true);
      expect(dedup.accept(3)).toBe(true);
      expect(dedup.accept(2)).toBe(false); // older than latest
    });
  });

  describe("reconnect backoff", () => {
    it("calculates bounded exponential backoff with jitter", () => {
      const b0 = calculateBackoff(0);
      expect(b0).toBeGreaterThanOrEqual(50);
      expect(b0).toBeLessThanOrEqual(200);

      const b1 = calculateBackoff(1);
      expect(b1).toBeGreaterThanOrEqual(100);
      expect(b1).toBeLessThanOrEqual(400);

      const b5 = calculateBackoff(5);
      expect(b5).toBeLessThanOrEqual(3000); // capped
    });
  });

  describe("privacy-safe errors", () => {
    it("never includes sensitive text or credentials in error messages", () => {
      const secret = "secret_bearer_token_12345";
      const userText = "My private password or medical question";
      const err = createPrivacySafeError(
        "edge_unavailable",
        `${secret} happened with ${userText}`,
      );
      expect(err.message).not.toContain(secret);
      expect(err.message).not.toContain(userText);
      expect(err.code).toBe("edge_unavailable");
    });
  });

  describe("single emission fallback analytics", () => {
    it("emits only server path when token exists", () => {
      const fetchSpy = vi
        .spyOn(globalThis, "fetch")
        .mockImplementation(() =>
          Promise.resolve(new Response(null, { status: 204 })),
        );
      const trackSpy = vi.spyOn(analyticsModule, "trackChatTransportFallback");

      notifyEdgeFallback(
        "tutor",
        "websocket",
        "sse",
        "upgrade_failed",
        "token-123",
      );

      expect(fetchSpy).toHaveBeenCalledTimes(1);
      expect(trackSpy).not.toHaveBeenCalled();
    });

    it("emits client wrapper only when token is absent", () => {
      const fetchSpy = vi.spyOn(globalThis, "fetch");
      const trackSpy = vi
        .spyOn(analyticsModule, "trackChatTransportFallback")
        .mockImplementation(() => {});

      notifyEdgeFallback(
        "tutor",
        "websocket",
        "engine",
        "edge_unavailable",
        undefined,
      );

      expect(fetchSpy).not.toHaveBeenCalled();
      expect(trackSpy).toHaveBeenCalledTimes(1);
    });
  });

  describe("guided chat edge transport", () => {
    it("invokes legacy fallback directly when flag is off", async () => {
      const fallback = vi.fn().mockResolvedValue({ next_step: "done" });
      const res = await sendGuidedTurn(
        "course_brief",
        { step: "intro" },
        fallback,
      );
      expect(fallback).toHaveBeenCalledTimes(1);
      expect(res).toEqual({ next_step: "done" });
    });
  });
});
