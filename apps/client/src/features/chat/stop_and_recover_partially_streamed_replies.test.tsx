import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { TutorChat } from "./TutorChat";
import {
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
/** A send that streams one delta and then waits for the abort signal. */
const hold: TutorService["send"] = (_c, _i, h: SendHandlers, signal) =>
  new Promise((_resolve, reject) => {
    h.onOpen();
    h.onThread("t1");
    h.onDelta("Earth turns");
    signal?.addEventListener("abort", () =>
      reject(new DOMException("aborted", "AbortError")),
    );
  });

it("Stop asks the server with the send's key before aborting and keeps the partial as stopped", async () => {
  const svc = service(hold, {
    turn: vi.fn().mockResolvedValue({
      state: "stopped",
      message: { id: "m-stored", content: "Earth turns", status: "stopped" },
    }),
  });
  await ask(svc);
  const stop = await screen.findByRole("button", { name: "Stop reply" });
  fireEvent.click(stop);
  await screen.findByText(
    "You stopped the reply. Only what was written is charged.",
    { exact: false },
  );
  const [, key] = svc.stop.mock.calls[0];
  expect(key).toBe((svc.send.mock.calls[0][1] as SendInput).idempotency_key);
  expect(svc.stop.mock.invocationCallOrder[0]).toBeLessThan(
    svc.turn.mock.invocationCallOrder[0],
  );
  expect(screen.getByText("Earth turns")).toBeTruthy();
  expect(screen.getByText(/Stopped · partial reply/)).toBeTruthy();
  // The composer is free again; a repeated stop has nothing to do.
  expect(
    (screen.getByRole("textbox", { name: "Message" }) as HTMLTextAreaElement)
      .disabled,
  ).toBe(false);
  expect(svc.stop).toHaveBeenCalledTimes(1);
});

it("Stop before the first token is available and keeps the draft with nothing stored", async () => {
  const pending: TutorService["send"] = (_c, _i, _h, signal) =>
    new Promise((_r, reject) =>
      signal?.addEventListener("abort", () =>
        reject(new DOMException("aborted", "AbortError")),
      ),
    );
  const svc = service(pending);
  const input = await ask(svc, "Tell me about Mars");
  const stop = await screen.findByRole("button", { name: "Stop reply" });
  expect((stop as HTMLButtonElement).disabled).toBe(false);
  fireEvent.click(stop);
  await waitFor(() => expect(svc.stop).toHaveBeenCalled());
  await screen.findByText(/You stopped the reply/);
  expect((input as HTMLTextAreaElement).value).toBe("Tell me about Mars");
  expect(
    screen.queryByText("Tell me about Mars", { selector: ".tutor-bubble" }),
  ).toBeNull();
});

it("a typed idle timeout shows the idle copy and Ask again starts a fresh send", async () => {
  let calls = 0;
  const svc = service(async (_c, _i, h) => {
    calls++;
    h.onOpen();
    h.onThread("t1");
    h.onDelta("Earth turns");
    if (calls === 1)
      throw new TutorStreamError("idle_timeout", true, "m1", true);
    return { message_id: "m2", thread_id: "t1", replayed: false };
  });
  await ask(svc);
  await screen.findByText(/The tutor stopped responding/);
  fireEvent.click(screen.getByRole("button", { name: /Ask again/ }));
  await waitFor(() => expect(svc.send).toHaveBeenCalledTimes(2));
  const [a, b] = svc.send.mock.calls.map((c) => c[1] as SendInput);
  expect(b.idempotency_key).not.toBe(a.idempotency_key);
  expect(b.quote_id).not.toBe(a.quote_id);
});

it("a dropped connection recovers the stored partial instead of the local text", async () => {
  const svc = service(
    async (_c, _i, h) => {
      h.onOpen();
      h.onDelta("Earth");
      throw new TypeError("network error");
    },
    {
      turn: vi.fn().mockResolvedValue({
        state: "interrupted",
        end_reason: "disconnect",
        message: {
          id: "m-stored",
          content: "Earth turns and spins",
          status: "interrupted",
        },
      }),
    },
  );
  await ask(svc);
  await screen.findByText("Earth turns and spins");
  expect(screen.getByText("Reply interrupted")).toBeTruthy();
  expect(svc.stop).not.toHaveBeenCalled();
});

it("a first-token failure keeps the draft and says nothing was charged", async () => {
  const svc = service(async () => {
    throw new TutorApiError(502, "first_token_timeout");
  });
  const input = await ask(svc, "Tell me about Mars");
  await screen.findByText(/Nothing was charged/);
  expect((input as HTMLTextAreaElement).value).toBe("Tell me about Mars");
  expect(svc.turn).not.toHaveBeenCalled();
});
