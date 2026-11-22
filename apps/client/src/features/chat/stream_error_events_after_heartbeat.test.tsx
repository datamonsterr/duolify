import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { TutorChat } from "./TutorChat";
import {
  TutorApiError,
  TutorStreamError,
  tutorService,
  type TutorService,
} from "./tutor";

const wallet = {
  available: 152,
  daily_free: {
    available: 28,
    granted: 30,
    resets_at: "2030-01-02T00:00:00Z",
    timezone: "UTC",
  },
  purchased: 124,
  subscription: 0,
  reserved: 0,
  recent_activity: [],
};
function service(
  send: TutorService["send"],
  overrides: Partial<TutorService> = {},
) {
  let quotes = 0;
  const base = {
    wallet: vi.fn().mockResolvedValue(wallet),
    thread: vi.fn().mockResolvedValue({ thread: null, messages: [] }),
    quote: vi.fn(async () => ({
      id: `q${++quotes}`,
      content_ceiling: 5,
      pricing_version: "v",
      daily_free_eligible: true,
      expires_at: new Date(Date.now() + 600000).toISOString(),
    })),
    accept: vi.fn().mockResolvedValue({ accepted: true }),
    send: vi.fn(send),
    remove: vi.fn().mockResolvedValue(undefined),
    saveReply: vi.fn(),
    stop: vi.fn().mockResolvedValue(undefined),
    turn: vi.fn().mockResolvedValue(null),
  };
  return Object.assign(base, overrides) as typeof base & TutorService;
}
async function ask(svc: TutorService, text = "Why?") {
  render(<TutorChat courseId="c1" service={svc} />);
  fireEvent.click(screen.getByRole("button", { name: "Ask the tutor" }));
  const input = screen.getByRole("textbox", { name: "Message" });
  fireEvent.change(input, { target: { value: text } });
  await screen.findByText(/up to 5 credits/i);
  fireEvent.keyDown(input, { key: "Enter" });
  return input;
}

// After a heartbeat the 200 is committed, so server failures arrive as an SSE `error` event.
/** A send whose stream opened (a heartbeat was sent) and then failed before any token. */
const openThenFail =
  (error: Error): TutorService["send"] =>
  async (_c, _i, h) => {
    h.onOpen();
    h.onThread("t1");
    throw error;
  };

it("an error event with code stopped gives the same stopped outcome as HTTP 409 stopped", async () => {
  const svc = service(
    openThenFail(new TutorStreamError("stopped", false, undefined, false)),
  );
  const input = await ask(svc, "Tell me about Mars");
  await screen.findByText(/You stopped the reply/);
  expect((input as HTMLTextAreaElement).value).toBe("Tell me about Mars");
  expect(screen.queryByText(/Nothing was charged/)).toBeNull();
});

it("an HTTP 409 stopped refusal also shows the stopped outcome, not a stale quote", async () => {
  const svc = service(async () => {
    throw new TutorApiError(409, "stopped");
  });
  await ask(svc);
  await screen.findByText(/You stopped the reply/);
});

it("an engine_unavailable error event keeps the draft like the HTTP 502 path", async () => {
  const svc = service(
    openThenFail(
      new TutorStreamError("engine_unavailable", false, undefined, true),
    ),
  );
  const input = await ask(svc, "Tell me about Mars");
  await screen.findByText(/Nothing was charged/);
  expect((input as HTMLTextAreaElement).value).toBe("Tell me about Mars");
  expect(svc.turn).not.toHaveBeenCalled();
});

it("a generic error event shows the failure notice and keeps the draft", async () => {
  const svc = service(openThenFail(new TutorStreamError("error", false)));
  const input = await ask(svc, "Tell me about Mars");
  await screen.findByText(/Nothing was charged/);
  expect((input as HTMLTextAreaElement).value).toBe("Tell me about Mars");
});

afterEach(() => vi.unstubAllGlobals());

it("tutorService.send turns heartbeat comments plus an error event into a typed TutorStreamError", async () => {
  const sse =
    ': keepalive\n\nevent: thread\ndata: {"thread_id":"t1"}\n\n: keepalive\n\nevent: error\ndata: {"code":"stopped","interrupted":false,"retryable":false}\n\n';
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async () =>
        new Response(sse, {
          status: 200,
          headers: { "content-type": "text/event-stream" },
        }),
    ),
  );
  const onOpen = vi.fn();
  const failure = await tutorService
    .send(
      "c1",
      { quote_id: "q", idempotency_key: "k", class: "short", message: "hi" },
      { onOpen, onThread: vi.fn(), onDelta: vi.fn() },
    )
    .catch((e: unknown) => e);
  expect(failure).toBeInstanceOf(TutorStreamError);
  expect(failure).toMatchObject({
    code: "stopped",
    interrupted: false,
    retryable: false,
  });
  expect(onOpen).toHaveBeenCalled();
});
