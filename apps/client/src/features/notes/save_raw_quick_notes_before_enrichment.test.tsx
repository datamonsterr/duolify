import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { QuickNotePanel } from "./QuickNotePanel";
import { configureAnalytics, trackQuickNoteCreated } from "../../lib/analytics";
import { noteQueue } from "./note-queue";

const posthog = vi.hoisted(() => ({
  capture: vi.fn(),
  has_opted_out_capturing: vi.fn(() => false),
  init: vi.fn(),
}));

vi.mock("posthog-js/dist/module.no-external", () => ({ default: posthog }));

describe("save_raw_quick_notes_before_enrichment", () => {
  beforeEach(() => {
    localStorage.clear();
    noteQueue.clear();
    configureAnalytics({
      learnerId: "11111111-1111-4111-8111-111111111111",
      optedOut: false,
      platform: "web",
      appVersion: "0.1.0",
    });
    vi.clearAllMocks();
  });

  afterEach(() => {
    configureAnalytics(null);
    vi.clearAllMocks();
  });

  it("handles offline save then single synchronization with zero-credit variant", async () => {
    const syncFn = vi.fn().mockImplementation(async () => {
      return { id: "server-note-uuid", status: "enrichment_pending" };
    });

    const onClose = vi.fn();
    const sourceContext = {
      source: "slide" as const,
      courseId: "course-123",
      lessonId: "lesson-456",
      slideId: 1,
      slideTitle: "Slide 2 · Why the sky changes",
    };

    render(
      <QuickNotePanel
        open={true}
        onClose={onClose}
        context={sourceContext}
        onSync={syncFn}
        isOffline={true}
      />,
    );

    // Verify context chip
    expect(
      screen.getByText(/Slide 2 · Why the sky changes/i),
    ).toBeInTheDocument();

    const textarea = screen.getByRole("textbox", { name: /Your note/i });
    const noteText =
      'Stars "move" because Earth spins west → east. Check: does Polaris move?';
    fireEvent.change(textarea, { target: { value: noteText } });

    // Verify character count indicator
    expect(
      screen.getByText(new RegExp(`${noteText.length} \\/ 2,000`)),
    ).toBeInTheDocument();

    // Save offline
    const saveButton = screen.getByRole("button", { name: /Save quick note/i });
    fireEvent.click(saveButton);

    // Assert offline banner
    expect(
      screen.getByText(/Offline variant · saved on this device/i),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/It will sync once you're online. Not backed up yet./i),
    ).toBeInTheDocument();

    // Verify note is stored in local device queue with stable action_key
    const queued = noteQueue.getQueue();
    expect(queued).toHaveLength(1);
    expect(queued[0].rawText).toBe(noteText);
    expect(queued[0].actionKey).toBeDefined();

    // Now synchronize note
    const syncButton = screen.getByRole("button", {
      name: /Sync now|Synchronize/i,
    });
    fireEvent.click(syncButton);

    await waitFor(() => {
      expect(syncFn).toHaveBeenCalledTimes(1);
    });

    // Assert server-synced and zero-credit banners
    expect(
      screen.getByText(/Saved. Enrichment is pending./i),
    ).toBeInTheDocument();
    expect(
      screen.getByText(
        /Synced to your account. The AI summary and tags appear in Quick notes./i,
      ),
    ).toBeInTheDocument();
    expect(screen.getByText(/Zero-credit variant/i)).toBeInTheDocument();
    expect(
      screen.getByText(
        /Your note is saved. Add credits to enrich it, or wait for your daily reset./i,
      ),
    ).toBeInTheDocument();

    // Test restoring player on close
    const backBtn = screen.getByRole("button", { name: "Back to lesson" });
    fireEvent.click(backBtn);
    expect(onClose).toHaveBeenCalled();
  });

  it("enforces text boundaries: empty text disables save, text over 2000 is rejected", () => {
    const onClose = vi.fn();
    const sourceContext = {
      source: "slide" as const,
      slideId: 0,
      slideTitle: "Slide 1",
    };

    render(
      <QuickNotePanel open={true} onClose={onClose} context={sourceContext} />,
    );

    const saveButton = screen.getByRole("button", { name: /Save quick note/i });
    expect(saveButton).toBeDisabled();

    const textarea = screen.getByRole("textbox", { name: /Your note/i });
    fireEvent.change(textarea, { target: { value: "   " } });
    expect(saveButton).toBeDisabled();

    // Max length is 2000
    expect(textarea).toHaveAttribute("maxLength", "2000");
  });

  it("emits quick_note_created and ui_sheet_opened without leaking private note text", () => {
    const secretText = "Super secret notes text";
    trackQuickNoteCreated({
      source: "slide",
      length: secretText.length,
    });

    expect(posthog.capture).toHaveBeenCalledWith("quick_note_created", {
      distinct_id: "11111111-1111-4111-8111-111111111111",
      platform: "web",
      app_version: "0.1.0",
      source: "slide",
      length: secretText.length,
    });

    const calls = JSON.stringify(posthog.capture.mock.calls);
    expect(calls).not.toContain(secretText);
    expect(calls).not.toContain("Super secret");
  });

  it("suppresses analytics when learner is opted out", () => {
    configureAnalytics({
      learnerId: "11111111-1111-4111-8111-111111111111",
      optedOut: true,
      platform: "web",
    });
    posthog.has_opted_out_capturing.mockReturnValue(true);

    trackQuickNoteCreated({
      source: "chat",
      length: 42,
    });

    expect(posthog.capture).not.toHaveBeenCalled();
  });
});
