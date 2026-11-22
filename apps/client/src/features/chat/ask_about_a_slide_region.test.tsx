import { act, renderHook, waitFor } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { useTutor } from "./useTutor";
import type { RegionAsk, SendInput, TutorService } from "./tutor";

const lesson = {
  lesson_id: "7d4b3c0e-8f0a-4a8e-9a53-0c1f6d9c2a11",
  slide_index: 2,
};
const region: RegionAsk = {
  rect: { x: 0.1, y: 0.2, w: 0.4, h: 0.3 },
  at_ms: 4200,
  input: "text",
};
const wallet = { available: 10 } as never;

function service() {
  const sent: SendInput[] = [];
  const svc = {
    wallet: vi.fn().mockResolvedValue(wallet),
    thread: vi.fn().mockResolvedValue({ thread: null, messages: [] }),
    quote: vi.fn().mockResolvedValue({
      id: "q1",
      content_ceiling: 2,
      pricing_version: "v",
      daily_free_eligible: true,
      expires_at: new Date(Date.now() + 600_000).toISOString(),
    }),
    accept: vi.fn().mockResolvedValue({}),
    send: vi.fn(async (_c: string, input: SendInput, h) => {
      sent.push(input);
      h.onOpen();
      h.onThread("t1");
      h.onDelta("ok");
      return { message_id: "m1", thread_id: "t1", replayed: false };
    }),
    stop: vi.fn(),
    turn: vi.fn(),
    remove: vi.fn(),
    saveReply: vi.fn(),
  } as unknown as TutorService;
  return { svc, sent };
}

it("sends the same region on the quote and on the message, next to the lesson", async () => {
  const { svc, sent } = service();
  const { result } = renderHook(() =>
    useTutor("c1", svc, true, lesson, region),
  );
  await waitFor(() => expect(result.current.loading).toBe(false));
  act(() => result.current.setDraft("What is this?"));
  await waitFor(() => expect(svc.quote).toHaveBeenCalled());
  expect(vi.mocked(svc.quote).mock.calls[0].slice(4)).toEqual([lesson, region]);
  await waitFor(() => expect(result.current.quote).toBeDefined());
  await act(async () => result.current.send());
  await waitFor(() => expect(sent).toHaveLength(1));
  expect(sent[0].lesson).toEqual(lesson);
  expect(sent[0].region).toEqual(region);
});

it("a lesson question without a region sends no region field", async () => {
  const { svc, sent } = service();
  const { result } = renderHook(() => useTutor("c1", svc, true, lesson));
  await waitFor(() => expect(result.current.loading).toBe(false));
  act(() => result.current.setDraft("What is this?"));
  await waitFor(() => expect(result.current.quote).toBeDefined());
  expect(vi.mocked(svc.quote).mock.calls[0].slice(4)).toEqual([lesson]);
  await act(async () => result.current.send());
  await waitFor(() => expect(sent).toHaveLength(1));
  expect("region" in sent[0]).toBe(false);
});

it("never sends a region without a lesson", async () => {
  const { svc } = service();
  const { result } = renderHook(() =>
    useTutor("c1", svc, true, undefined, region),
  );
  await waitFor(() => expect(result.current.loading).toBe(false));
  act(() => result.current.setDraft("Hello"));
  await waitFor(() => expect(svc.quote).toHaveBeenCalled());
  expect(vi.mocked(svc.quote).mock.calls[0].slice(4)).toEqual([]);
});
