import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { TutorChat } from "./TutorChat";
import type { TutorService } from "./tutor";
import { configureAnalytics } from "../../lib/analytics";
import { NotesPage } from "../notes/NotesPage";
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
  available: 0,
  daily_free: {
    available: 0,
    granted: 30,
    resets_at: "2030-01-02T00:00:00Z",
    timezone: "UTC",
  },
  purchased: 0,
  subscription: 0,
  reserved: 0,
  recent_activity: [],
};
const stored = (
  id: string,
  role: "user" | "assistant",
  content: string,
  status = "complete",
) => ({
  id,
  role,
  content,
  status,
  created_at: "2030-01-01T00:00:00Z",
});
function service(overrides: Partial<TutorService> = {}) {
  const base = {
    wallet: vi.fn().mockResolvedValue(wallet),
    thread: vi.fn().mockResolvedValue({
      thread: { id: "t1" },
      messages: [
        stored(
          "m-user",
          "user",
          "Why does the North Star stay still? Private words",
        ),
        stored("m-ai", "assistant", "Polaris sits above the pole."),
        stored("m-stopped", "assistant", "Partly", "stopped"),
        stored("m-cut", "assistant", "Cut off", "interrupted"),
      ],
    }),
    quote: vi.fn(),
    accept: vi.fn(),
    send: vi.fn(),
    remove: vi.fn().mockResolvedValue(undefined),
    stop: vi.fn().mockResolvedValue(undefined),
    turn: vi.fn().mockResolvedValue(null),
    saveReply: vi.fn().mockResolvedValue({
      id: "n1",
      status: "enrichment_pending",
      truncated: false,
    }),
  };
  return Object.assign(base, overrides) as typeof base & TutorService;
}
const open = async (svc: TutorService, withLesson = true) => {
  render(
    <TutorChat
      courseId="c1"
      lesson={withLesson ? lesson : undefined}
      embedded
      service={svc}
    />,
  );
  await screen.findByText("Polaris sits above the pole.");
};

beforeEach(() => {
  sessionStorage.clear();
  posthog.capture.mockClear();
  configureAnalytics({ learnerId: learner, optedOut: false, platform: "web" });
});
afterEach(() => configureAnalytics(null));

it("offers Save only on complete or stopped assistant replies and saves each once", async () => {
  const svc = service();
  await open(svc);
  const saves = screen.getAllByRole("button", { name: "Save as quick note" });
  expect(saves).toHaveLength(2);
  fireEvent.click(saves[0]);
  fireEvent.click(saves[0]);
  await screen.findByText("Saved to Quick notes · Enrichment pending");
  // A double click while saving sends one request, carrying the message and lesson ids only.
  expect(svc.saveReply).toHaveBeenCalledTimes(1);
  expect(svc.saveReply).toHaveBeenCalledWith("m-ai", lesson.lesson_id);
  expect(
    screen.getAllByRole("button", { name: "Save as quick note" }),
  ).toHaveLength(1);
  expect(screen.getByRole("link", { name: "Open notes" })).toBeTruthy();
});

it("a failed save keeps the reply savable and a retry addresses the same message", async () => {
  const saveReply = vi
    .fn()
    .mockRejectedValueOnce(new Error("network"))
    .mockResolvedValue({
      id: "n1",
      status: "enrichment_pending",
      truncated: false,
    });
  const svc = service({ saveReply });
  await open(svc, false);
  fireEvent.click(
    screen.getAllByRole("button", { name: "Save as quick note" })[0],
  );
  await screen.findByText("Could not save the note. Try again.");
  fireEvent.click(
    screen.getAllByRole("button", { name: "Save as quick note" })[0],
  );
  await screen.findByText("Saved to Quick notes · Enrichment pending");
  expect(saveReply.mock.calls.map((c) => c[0])).toEqual(["m-ai", "m-ai"]);
  // Without a lesson context no lesson id is sent.
  expect(saveReply.mock.calls[0][1]).toBeUndefined();
});

it("a truncated save says so", async () => {
  const svc = service({
    saveReply: vi.fn().mockResolvedValue({
      id: "n1",
      status: "enrichment_pending",
      truncated: true,
    }),
  });
  await open(svc);
  fireEvent.click(
    screen.getAllByRole("button", { name: "Save as quick note" })[0],
  );
  await screen.findByText(/shortened to fit a note/i);
});

it("Save and Delete are unavailable offline", async () => {
  const svc = service();
  await open(svc);
  window.dispatchEvent(new Event("offline"));
  await waitFor(() =>
    expect(
      (
        screen.getAllByRole("button", {
          name: "Save as quick note",
        })[0] as HTMLButtonElement
      ).disabled,
    ).toBe(true),
  );
  expect(
    (screen.getByRole("button", { name: "Delete chat" }) as HTMLButtonElement)
      .disabled,
  ).toBe(true);
  window.dispatchEvent(new Event("online"));
});

it("delete names the thread and its messages, keeps focus rules, and emits one text-free event", async () => {
  const svc = service();
  await open(svc);
  const opener = screen.getByRole("button", { name: "Delete chat" });
  fireEvent.click(opener);
  const dialog = await screen.findByRole("dialog", {
    name: "Delete this chat?",
  });
  expect(dialog.textContent).toContain("Why does the North Star stay");
  expect(dialog.textContent).toContain("4 messages");
  expect(dialog.textContent).toContain("Quick notes you saved from it stay");
  expect(document.activeElement).toBe(
    screen.getByRole("button", { name: "Cancel" }),
  );
  fireEvent.keyDown(dialog, { key: "Escape" });
  await waitFor(() =>
    expect(
      screen.queryByRole("dialog", { name: "Delete this chat?" }),
    ).toBeNull(),
  );
  expect(document.activeElement).toBe(opener);
  expect(svc.remove).not.toHaveBeenCalled();
  expect(posthog.capture).not.toHaveBeenCalled();

  fireEvent.click(opener);
  fireEvent.click(await screen.findByRole("button", { name: /^Delete$/ }));
  await waitFor(() => expect(svc.remove).toHaveBeenCalledWith("t1"));
  expect(posthog.capture).toHaveBeenCalledTimes(1);
  expect(posthog.capture).toHaveBeenCalledWith("ui_control_activated", {
    distinct_id: learner,
    platform: "web",
    app_version: "0.1.0",
    source: "client",
    surface: "tutor_chat",
    control_id: "delete_thread",
  });
  expect(JSON.stringify(posthog.capture.mock.calls)).not.toContain("Private");
  await waitFor(() =>
    expect(screen.queryByText("Polaris sits above the pole.")).toBeNull(),
  );
});

it("a failed delete keeps the thread, shows the error and emits nothing more on retry cancel", async () => {
  const remove = vi
    .fn()
    .mockRejectedValueOnce(new Error("503"))
    .mockResolvedValue(undefined);
  const svc = service({ remove });
  await open(svc);
  fireEvent.click(screen.getByRole("button", { name: "Delete chat" }));
  fireEvent.click(await screen.findByRole("button", { name: /^Delete$/ }));
  await screen.findByText("Could not delete the chat. Try again.");
  expect(screen.getByText("Polaris sits above the pole.")).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: /^Delete$/ }));
  await waitFor(() => expect(remove).toHaveBeenCalledTimes(2));
});

it("honours the analytics opt-out for the delete event", async () => {
  configureAnalytics({ learnerId: learner, optedOut: true, platform: "web" });
  const svc = service();
  await open(svc);
  fireEvent.click(screen.getByRole("button", { name: "Delete chat" }));
  fireEvent.click(await screen.findByRole("button", { name: /^Delete$/ }));
  await waitFor(() => expect(svc.remove).toHaveBeenCalled());
  expect(posthog.capture).not.toHaveBeenCalled();
});

it("notes saved from a deleted thread stay readable and say the source is unavailable", async () => {
  const note = (id: string, chat_source: object) => ({
    id,
    action_key: `tutor-message:${id}`,
    source: "chat" as const,
    raw_text: `Saved text ${id}`,
    status: "enrichment_pending" as const,
    reviewed: false,
    version: 1,
    chat_source,
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
  });
  vi.spyOn(session, "request").mockResolvedValue([
    note("gone", { state: "unavailable" }),
    note("live", { state: "available", thread_id: "t", message_id: "m" }),
  ]);
  render(<NotesPage />);
  expect(
    (await screen.findAllByText("Saved text gone")).length,
  ).toBeGreaterThan(0);
  expect(screen.getAllByText("Saved text live").length).toBeGreaterThan(0);
  // The list row and the open detail both name the unavailable source; the live note never does.
  const labels = screen.getAllByText(/source unavailable/i);
  expect(labels.length).toBeGreaterThan(0);
  expect(screen.getAllByText(/From tutor chat$/).length).toBeGreaterThan(0);
  vi.restoreAllMocks();
});

it("a native cancel or Escape on the confirm closes only the confirm and returns focus to Delete", async () => {
  const outerCancel = vi.fn();
  const outerKey = vi.fn();
  const svc = service();
  render(
    // Stands in for the modal sheet's <dialog>, which closes itself on cancel and Escape.
    <div onKeyDown={outerKey}>
      <TutorChat courseId="c1" lesson={lesson} embedded service={svc} />
    </div>,
  );
  await screen.findByText("Polaris sits above the pole.");
  document.addEventListener("cancel", outerCancel);
  const opener = screen.getByRole("button", { name: "Delete chat" });
  fireEvent.click(opener);
  const dialog = await screen.findByRole("dialog", {
    name: "Delete this chat?",
  });
  // The browser's close request for a modal <dialog> arrives as a cancelable `cancel` event.
  const cancel = new Event("cancel", { cancelable: true });
  fireEvent(dialog, cancel);
  expect(cancel.defaultPrevented).toBe(true);
  await waitFor(() =>
    expect(
      screen.queryByRole("dialog", { name: "Delete this chat?" }),
    ).toBeNull(),
  );
  expect(document.activeElement).toBe(opener);
  // Escape reaching the confirm never bubbles to the sheet behind it.
  fireEvent.click(opener);
  fireEvent.keyDown(await screen.findByRole("button", { name: "Cancel" }), {
    key: "Escape",
  });
  expect(outerKey).not.toHaveBeenCalled();
  expect(
    screen.queryByRole("dialog", { name: "Delete this chat?" }),
  ).toBeNull();
  expect(document.activeElement).toBe(opener);
  document.removeEventListener("cancel", outerCancel);
});
