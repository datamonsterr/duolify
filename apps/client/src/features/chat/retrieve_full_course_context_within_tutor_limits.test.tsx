import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { TutorChat } from "./TutorChat";
import {
  TutorApiError,
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
const approval = () =>
  new TutorApiError(422, "context_approval_required", {
    required_tokens: 20000,
    standard_limit_tokens: 12500,
    prompt_limit_tokens: 48000,
  });
function service(overrides: Partial<TutorService> = {}) {
  let quotes = 0;
  const base = {
    wallet: vi.fn().mockResolvedValue(wallet),
    thread: vi.fn().mockResolvedValue({ thread: null, messages: [] }),
    quote: vi.fn(
      async (_course: string, _cls: string, _text: string, _ok?: boolean) => ({
        id: `q${++quotes}`,
        content_ceiling: 5,
        pricing_version: "v",
        daily_free_eligible: true,
        expires_at: new Date(Date.now() + 600000).toISOString(),
      }),
    ),
    accept: vi.fn().mockResolvedValue({ accepted: true }),
    send: vi.fn(async (_c: string, _i: SendInput, h: SendHandlers) => {
      h.onOpen();
      h.onThread("t1");
      h.onDelta("Zorblax is the founding constant.");
      return { message_id: "m1", thread_id: "t1", replayed: false };
    }),
    remove: vi.fn().mockResolvedValue(undefined),
    stop: vi.fn().mockResolvedValue(undefined),
    turn: vi.fn().mockResolvedValue(null),
  };
  return Object.assign(base, overrides) as typeof base & TutorService;
}
function open(svc: TutorService) {
  render(<TutorChat courseId="c1" service={svc} />);
  fireEvent.click(screen.getByRole("button", { name: "Ask the tutor" }));
  return screen.getByRole("textbox", { name: "Message" });
}

it("asks for explicit approval when the course needs a larger context, then quotes with the approval", async () => {
  const quote = vi
    .fn()
    .mockRejectedValueOnce(approval())
    .mockResolvedValue({
      id: "q-approved",
      content_ceiling: 5,
      pricing_version: "v",
      daily_free_eligible: true,
      expires_at: new Date(Date.now() + 600000).toISOString(),
    });
  const svc = service({ quote });
  const input = open(svc);
  fireEvent.change(input, { target: { value: "What is zorblax?" } });
  const alert = await screen.findByRole("alert");
  expect(alert.textContent).toMatch(/larger context/i);
  expect(alert.textContent).toMatch(/20,000/);
  // Without approval nothing is sent, not even by Enter.
  fireEvent.keyDown(input, { key: "Enter" });
  expect(svc.send).not.toHaveBeenCalled();
  expect(svc.accept).not.toHaveBeenCalled();
  expect(quote).toHaveBeenCalledTimes(1);
  expect(quote.mock.calls[0][3] ?? false).toBe(false);

  fireEvent.click(
    screen.getByRole("button", { name: "Approve larger context" }),
  );
  await screen.findByText(/up to 5 credits/i);
  expect(quote).toHaveBeenLastCalledWith(
    "c1",
    "short",
    "What is zorblax?",
    true,
  );
  expect(screen.queryByRole("alert")).toBeNull();
  await waitFor(() => expect(document.activeElement).toBe(input));
  fireEvent.keyDown(input, { key: "Enter" });
  expect(
    await screen.findByText("Zorblax is the founding constant."),
  ).toBeTruthy();
  expect(svc.send.mock.calls[0][1].quote_id).toBe("q-approved");
});

it("an oversized course gets a typed too-large answer without an approve action, and editing retries", async () => {
  const quote = vi
    .fn()
    .mockRejectedValueOnce(new TutorApiError(422, "context_too_large", {}))
    .mockResolvedValue({
      id: "q2",
      content_ceiling: 5,
      pricing_version: "v",
      daily_free_eligible: true,
      expires_at: new Date(Date.now() + 600000).toISOString(),
    });
  const svc = service({ quote });
  const input = open(svc);
  fireEvent.change(input, { target: { value: "Everything?" } });
  const alert = await screen.findByRole("alert");
  expect(alert.textContent).toMatch(/too large/i);
  expect(alert.textContent).toMatch(/Nothing was charged/i);
  expect(
    screen.queryByRole("button", { name: "Approve larger context" }),
  ).toBeNull();
  fireEvent.keyDown(input, { key: "Enter" });
  expect(svc.send).not.toHaveBeenCalled();
  expect((input as HTMLTextAreaElement).value).toBe("Everything?");
  fireEvent.change(input, { target: { value: "Just the first lesson?" } });
  await screen.findByText(/up to 5 credits/i);
  expect(screen.queryByRole("alert")).toBeNull();
});

it("a send-time approval answer keeps the draft and offers approval", async () => {
  const svc = service({ send: vi.fn().mockRejectedValue(approval()) });
  const input = open(svc);
  fireEvent.change(input, { target: { value: "Question" } });
  await screen.findByText(/up to 5 credits/i);
  fireEvent.keyDown(input, { key: "Enter" });
  const alert = await screen.findByRole("alert");
  expect(alert.textContent).toMatch(/larger context/i);
  expect((input as HTMLTextAreaElement).value).toBe("Question");
  expect(
    screen.getByRole("button", { name: "Approve larger context" }),
  ).toBeTruthy();
});
