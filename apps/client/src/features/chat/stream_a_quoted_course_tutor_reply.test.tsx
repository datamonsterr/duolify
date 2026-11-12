import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { TutorChat } from "./TutorChat";
import {
  parseSse,
  TutorApiError,
  TutorStreamError,
  type SendHandlers,
  type SendInput,
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
function service(overrides: Partial<TutorService> = {}) {
  let quotes = 0;
  const base = {
    wallet: vi.fn().mockResolvedValue(wallet),
    thread: vi.fn().mockResolvedValue({ thread: null, messages: [] }),
    quote: vi.fn(async (_course: string, cls: string) => ({
      id: `q${++quotes}`,
      content_ceiling: cls === "long" ? 8 : 5,
      pricing_version: "v",
      daily_free_eligible: true,
      expires_at: new Date(Date.now() + 600000).toISOString(),
    })),
    accept: vi.fn().mockResolvedValue({ accepted: true }),
    send: vi.fn(async (_course: string, _input: SendInput, h: SendHandlers) => {
      h.onOpen();
      h.onThread("t1");
      h.onDelta("Polaris ");
      h.onDelta("is far.");
      return { message_id: "m1", thread_id: "t1", replayed: false };
    }),
    remove: vi.fn().mockResolvedValue(undefined),
    stop: vi.fn().mockResolvedValue(undefined),
    turn: vi.fn().mockResolvedValue(null),
  };
  return Object.assign(base, overrides) as typeof base & TutorService;
}
const enter = (input: HTMLElement) =>
  fireEvent.keyDown(input, { key: "Enter" });
async function typeAndWaitForQuote(text: string, credits = 5) {
  const input = screen.getByRole("textbox", { name: "Message" });
  fireEvent.change(input, { target: { value: text } });
  await screen.findByText(new RegExp(`up to ${credits} credits`, "i"));
  return input;
}
function open(svc: TutorService) {
  render(<TutorChat courseId="c1" service={svc} />);
  fireEvent.click(screen.getByRole("button", { name: "Ask the tutor" }));
}

it("parses SSE events split across chunks, CRLF and multi-line data", async () => {
  const encoder = new TextEncoder();
  const text =
    'event: thread\r\ndata: {"thread_id":"t"}\r\n\r\nevent: delta\ndata: {"text":\ndata: "a"}\n\n: comment\nevent: done\ndata: {"message_id":"m"}\n\n';
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      for (let i = 0; i < text.length; i += 7)
        controller.enqueue(encoder.encode(text.slice(i, i + 7)));
      controller.close();
    },
  });
  const events = [];
  for await (const event of parseSse(stream)) events.push(event);
  expect(events).toEqual([
    { event: "thread", data: { thread_id: "t" } },
    { event: "delta", data: { text: "a" } },
    { event: "done", data: { message_id: "m" } },
  ]);
});

it("quotes, accepts then streams; Enter sends and Shift+Enter adds a line", async () => {
  const svc = service();
  open(svc);
  const input = await typeAndWaitForQuote("Why?");
  expect(svc.wallet).toHaveBeenCalled();
  expect(screen.getByText(/Daily free 28 · Purchased 124/)).toBeTruthy();
  fireEvent.keyDown(input, { key: "Enter", shiftKey: true });
  fireEvent.change(input, { target: { value: "Why?\nmore" } });
  expect(svc.send).not.toHaveBeenCalled();
  expect((input as HTMLTextAreaElement).value).toBe("Why?\nmore");
  await screen.findByText(/up to 5 credits/i);
  enter(input);
  expect(await screen.findByText("Polaris is far.")).toBeTruthy();
  expect(svc.accept.mock.invocationCallOrder[0]).toBeLessThan(
    svc.send.mock.invocationCallOrder[0],
  );
  const [, sent] = svc.send.mock.calls[0];
  expect(sent).toMatchObject({
    class: "short",
    message: "Why?\nmore",
    quote_id: expect.stringMatching(/^q/),
  });
  expect(sent.idempotency_key).toMatch(/^[0-9a-f-]{36}$/);
  expect((input as HTMLTextAreaElement).value).toBe("");
  expect(screen.getByRole("log").getAttribute("aria-live")).toBe("polite");
});

it("keeps the draft and shows the upsell on insufficient credits", async () => {
  const svc = service({
    send: vi.fn().mockRejectedValue(
      new TutorApiError(402, "insufficient_credits", {
        next_grant_at: "2030-01-02T00:00:00Z",
      }),
    ),
  });
  open(svc);
  const input = await typeAndWaitForQuote("How far?");
  enter(input);
  const alert = await screen.findByRole("alert");
  expect(alert.textContent).toMatch(/Not enough credits/);
  expect(alert.textContent).toMatch(/Next daily free credits/);
  expect(
    screen
      .getByRole("link", { name: "See credit options" })
      .getAttribute("href"),
  ).toBe("/credits");
  expect((input as HTMLTextAreaElement).value).toBe("How far?");
  expect(screen.queryByRole("log")?.textContent ?? "").not.toMatch(/How far/);
});

it("fetches a fresh quote after a stale-quote 409 and keeps the draft", async () => {
  const svc = service({
    send: vi
      .fn()
      .mockRejectedValueOnce(new TutorApiError(409, "quote_used", {}))
      .mockImplementation(async (_c, _i, h: SendHandlers) => {
        h.onOpen();
        h.onDelta("ok");
        return { message_id: "m", thread_id: "t", replayed: false };
      }),
  });
  open(svc);
  const input = await typeAndWaitForQuote("Hi");
  enter(input);
  await waitFor(() => expect(svc.quote.mock.calls.length).toBeGreaterThan(1));
  expect((input as HTMLTextAreaElement).value).toBe("Hi");
  enter(input);
  expect(await screen.findByText("ok")).toBeTruthy();
  const keys = svc.send.mock.calls.map(([, i]) => i.idempotency_key);
  expect(new Set(keys).size).toBe(2);
});

it("shows an interrupted prefix and retries with a new key and fresh quote", async () => {
  const svc = service({
    send: vi
      .fn()
      .mockImplementationOnce(async (_c, _i, h: SendHandlers) => {
        h.onOpen();
        h.onDelta("Earth turns");
        throw new TutorStreamError("engine_failed", true, "m1");
      })
      .mockImplementation(async (_c, _i, h: SendHandlers) => {
        h.onOpen();
        h.onDelta("Whole answer.");
        return { message_id: "m2", thread_id: "t", replayed: false };
      }),
  });
  open(svc);
  const input = await typeAndWaitForQuote("Why?");
  enter(input);
  expect(await screen.findByText("Earth turns")).toBeTruthy();
  expect(screen.getByText("Reply interrupted")).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: /Ask again/ }));
  expect(await screen.findByText("Whole answer.")).toBeTruthy();
  const [first, second] = svc.send.mock.calls.map(([, i]) => i);
  expect(second.idempotency_key).not.toBe(first.idempotency_key);
  expect(second.quote_id).not.toBe(first.quote_id);
  expect(second.message).toBe("Why?");
});

it("reports a pre-stream failure as uncharged and retries", async () => {
  const svc = service({
    send: vi
      .fn()
      .mockRejectedValueOnce(new TutorApiError(502, "engine_unavailable", {}))
      .mockImplementation(async (_c, _i, h: SendHandlers) => {
        h.onOpen();
        h.onDelta("Back.");
        return { message_id: "m", thread_id: "t", replayed: false };
      }),
  });
  open(svc);
  const input = await typeAndWaitForQuote("Hello");
  enter(input);
  expect((await screen.findByRole("alert")).textContent).toMatch(
    /Nothing was charged/,
  );
  expect((input as HTMLTextAreaElement).value).toBe("Hello");
  fireEvent.click(screen.getByRole("button", { name: "Try again" }));
  expect(await screen.findByText("Back.")).toBeTruthy();
});

it("restores a thread and deletes it only after confirmation", async () => {
  const svc = service({
    thread: vi.fn().mockResolvedValue({
      thread: { id: "t9" },
      messages: [
        {
          id: "a",
          role: "user",
          content: "Earlier",
          status: "complete",
          created_at: "x",
        },
        {
          id: "b",
          role: "assistant",
          content: "Answer",
          status: "complete",
          created_at: "x",
        },
      ],
    }),
  });
  open(svc);
  expect(await screen.findByText("Earlier")).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "Delete chat" }));
  expect(svc.remove).not.toHaveBeenCalled();
  fireEvent.click(
    within(screen.getByRole("dialog")).getByRole("button", { name: "Delete" }),
  );
  await waitFor(() => expect(svc.remove).toHaveBeenCalledWith("t9"));
  await waitFor(() => expect(screen.queryByText("Earlier")).toBeNull());
});
