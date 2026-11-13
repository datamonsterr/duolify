import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { TutorChat } from "./TutorChat";
import {
  TutorApiError,
  type SendHandlers,
  type SendInput,
  type TutorService,
} from "./tutor";
import { configureAnalytics, trackGuidedUi } from "../../lib/analytics";
import { session } from "../accounts/session";

const posthog = vi.hoisted(() => ({
  capture: vi.fn(),
  has_opted_out_capturing: vi.fn(() => false),
  init: vi.fn(),
}));
vi.mock("posthog-js/dist/module.no-external", () => ({ default: posthog }));

const learner = "11111111-1111-4111-8111-111111111111";
const lesson = {
  lesson_id: "7d4b3c0e-8f0a-4a8e-9a53-0c1f6d9c2a11",
  slide_index: 2,
};
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
function service(send: TutorService["send"]) {
  return {
    wallet: vi.fn().mockResolvedValue(wallet),
    thread: vi.fn().mockResolvedValue({ thread: null, messages: [] }),
    quote: vi.fn(async () => ({
      id: "q1",
      content_ceiling: 5,
      pricing_version: "v",
      daily_free_eligible: true,
      expires_at: new Date(Date.now() + 600000).toISOString(),
    })),
    accept: vi.fn().mockResolvedValue({ accepted: true }),
    send: vi.fn(send),
    remove: vi.fn().mockResolvedValue(undefined),
    stop: vi.fn().mockResolvedValue(undefined),
    turn: vi.fn().mockResolvedValue(null),
  };
}
const ok: TutorService["send"] = async (_c, _i, h: SendHandlers) => {
  h.onOpen();
  h.onThread("t1");
  h.onDelta("Look at the ");
  h.onDelta("second star.");
  return { message_id: "m1", thread_id: "t1", replayed: false };
};
async function typeAndSend(text: string) {
  const input = screen.getByRole("textbox", { name: "Message" });
  fireEvent.change(input, { target: { value: text } });
  await screen.findByText(/up to 5 credits/i);
  fireEvent.keyDown(input, { key: "Enter" });
  return input;
}

beforeEach(() => {
  sessionStorage.clear();
  vi.clearAllMocks();
  configureAnalytics({
    learnerId: learner,
    optedOut: false,
    platform: "web",
    appVersion: "0.1.0",
  });
});
afterEach(() => configureAnalytics(null));

it("embedded in the lesson sheet it has no launcher and sends only a lesson reference", async () => {
  const svc = service(ok);
  render(<TutorChat courseId="c1" lesson={lesson} embedded service={svc} />);
  expect(screen.queryByRole("button", { name: "Ask the tutor" })).toBeNull();
  await typeAndSend("What is this star?");
  await screen.findByText("Look at the second star.");
  expect(svc.quote).toHaveBeenCalledWith(
    "c1",
    "short",
    "What is this star?",
    false,
    lesson,
  );
  const input = svc.send.mock.calls[0][1] as SendInput;
  expect(input.lesson).toEqual(lesson);
  expect(Object.keys(input.lesson!).sort()).toEqual([
    "lesson_id",
    "slide_index",
  ]);
});

it("without a lesson the Course-level tutor sends no lesson field", async () => {
  const svc = service(ok);
  render(<TutorChat courseId="c1" service={svc} />);
  fireEvent.click(screen.getByRole("button", { name: "Ask the tutor" }));
  await typeAndSend("Hello");
  await screen.findByText("Look at the second star.");
  expect("lesson" in (svc.send.mock.calls[0][1] as SendInput)).toBe(false);
});

it("announces start and finish once and keeps the log silent while streaming", async () => {
  let release: () => void = () => undefined;
  const svc = service(async (_c, _i, h) => {
    h.onOpen();
    h.onDelta("Because air");
    await new Promise<void>((resolve) => (release = resolve));
    return { message_id: "m1", thread_id: "t1", replayed: false };
  });
  render(<TutorChat courseId="c1" lesson={lesson} embedded service={svc} />);
  await typeAndSend("Why does it twinkle?");
  const log = await screen.findByRole("log");
  const status = await screen.findByText("Tutor is answering");
  expect(log.getAttribute("aria-live")).toBe("off");
  expect(status.closest("[role=status]")?.textContent).not.toContain("Because");
  expect(screen.getByRole("button", { name: "Stop reply" })).toBeTruthy();
  await act(async () => release());
  await screen.findByText("Tutor replied");
  expect(log.getAttribute("aria-live")).toBe("polite");
  expect(screen.queryByText("Tutor is answering")).toBeNull();
});

it("zero credits keep the draft, show the reset and top-up, and never resend", async () => {
  const svc = service(async () => {
    throw new TutorApiError(402, "insufficient_credits", {
      next_grant_at: "2030-01-02T00:00:00Z",
    });
  });
  render(<TutorChat courseId="c1" lesson={lesson} embedded service={svc} />);
  const input = await typeAndSend("Explain parallax");
  await screen.findByText("Not enough credits");
  expect(screen.getByText(/Next daily free credits/)).toBeTruthy();
  expect(screen.getByRole("link", { name: "See credit options" })).toBeTruthy();
  expect((input as HTMLTextAreaElement).value).toBe("Explain parallax");
  expect(sessionStorage.getItem("tutor-draft::c1")).toBe("Explain parallax");
  await new Promise((r) => setTimeout(r, 700));
  expect(svc.send).toHaveBeenCalledTimes(1);
});

it("emits recovery selections without learner text and honours opt-out", async () => {
  const svc = service(async () => {
    throw new TutorApiError(402, "insufficient_credits", {});
  });
  render(<TutorChat courseId="c1" lesson={lesson} embedded service={svc} />);
  await typeAndSend("Private question");
  const link = await screen.findByRole("link", { name: "See credit options" });
  link.addEventListener("click", (event) => event.preventDefault());
  fireEvent.click(link);
  expect(posthog.capture).toHaveBeenCalledWith("ui_recovery_selected", {
    distinct_id: learner,
    platform: "web",
    app_version: "0.1.0",
    source: "client",
    surface: "lesson_tutor",
    action: "top_up",
  });
  expect(JSON.stringify(posthog.capture.mock.calls)).not.toContain("Private");
  posthog.capture.mockClear();
  configureAnalytics({
    learnerId: learner,
    optedOut: true,
    platform: "web",
  });
  fireEvent.click(link);
  expect(posthog.capture).not.toHaveBeenCalled();
});

it("registers the lesson_tutor sheet and rejects unknown ids", () => {
  trackGuidedUi({ event: "ui_sheet_opened", sheetId: "lesson_tutor" });
  expect(posthog.capture).toHaveBeenCalledWith("ui_sheet_opened", {
    distinct_id: learner,
    platform: "web",
    app_version: "0.1.0",
    source: "client",
    sheet_id: "lesson_tutor",
  });
  posthog.capture.mockClear();
  trackGuidedUi({ event: "ui_sheet_opened", sheetId: "my private" } as never);
  expect(posthog.capture).not.toHaveBeenCalled();
});

it("learner-scoped tutor drafts are removed on logout", () => {
  sessionStorage.setItem("tutor-draft:learner-a:c1", "unsent question");
  sessionStorage.setItem("tutor-draft:learner-a:c2", "another");
  sessionStorage.setItem("unrelated", "keep");
  session.clear();
  expect(sessionStorage.getItem("tutor-draft:learner-a:c1")).toBeNull();
  expect(sessionStorage.getItem("tutor-draft:learner-a:c2")).toBeNull();
  expect(sessionStorage.getItem("unrelated")).toBe("keep");
});
