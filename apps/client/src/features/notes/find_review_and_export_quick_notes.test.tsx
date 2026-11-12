import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { NotesPage } from "./NotesPage";
import { session } from "../accounts/session";
import { configureAnalytics } from "../../lib/analytics";

const posthog = vi.hoisted(() => ({
  capture: vi.fn(),
  has_opted_out_capturing: vi.fn(() => false),
  init: vi.fn(),
}));

vi.mock("posthog-js/dist/module.no-external", () => ({ default: posthog }));

describe("find_review_and_export_quick_notes", () => {
  const learnerId = "11111111-1111-4111-8111-111111111111";

  beforeEach(() => {
    localStorage.clear();
    sessionStorage.clear();
    configureAnalytics({
      learnerId,
      optedOut: false,
      platform: "web",
      appVersion: "0.1.0",
    });
    vi.clearAllMocks();
  });

  afterEach(() => {
    configureAnalytics(null);
    vi.restoreAllMocks();
  });

  it("filters notes by search text, tags, and reviewed state, showing result counts and clear filters", async () => {
    const mockNotes = [
      {
        id: "note-1",
        action_key: "key-1",
        course_id: "course-astronomy",
        lesson_id: "lesson-1",
        slide_title: "Why the sky changes · Slide 2",
        source: "slide" as const,
        raw_text: "Stars move because Earth spins west to east.",
        status: "enriched" as const,
        reviewed: false,
        reviewed_at: null,
        summary: "Earth rotation causes apparent star motion.",
        tags: ["rotation", "polaris"],
        links: ["lesson-2"],
        version: 1,
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
      },
      {
        id: "note-2",
        action_key: "key-2",
        course_id: "course-plants",
        lesson_id: "lesson-2",
        slide_title: "Watering schedules",
        source: "chat" as const,
        raw_text: "Water plants in the morning.",
        status: "raw_only" as const,
        reviewed: true,
        reviewed_at: new Date().toISOString(),
        version: 1,
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
      },
    ];

    vi.spyOn(session, "request").mockResolvedValue(mockNotes);

    render(<NotesPage />);

    // Initially both notes should be present
    expect(
      (
        await screen.findAllByText(
          "Stars move because Earth spins west to east.",
        )
      )[0],
    ).toBeInTheDocument();
    expect(
      screen.getByText("Water plants in the morning."),
    ).toBeInTheDocument();

    // Search for "Earth"
    const searchInput = screen.getByPlaceholderText("Search your notes");
    fireEvent.change(searchInput, { target: { value: "Earth" } });

    expect(
      screen.getAllByText("Stars move because Earth spins west to east.")[0],
    ).toBeInTheDocument();
    expect(
      screen.queryByText("Water plants in the morning."),
    ).not.toBeInTheDocument();

    // Clear filters
    const clearBtn = screen.getByRole("button", { name: /Clear filters/i });
    fireEvent.click(clearBtn);

    expect(
      screen.getByText("Water plants in the morning."),
    ).toBeInTheDocument();

    // Filter by "Not reviewed"
    const notReviewedBtn = screen.getByRole("button", {
      name: /Not reviewed/i,
    });
    fireEvent.click(notReviewedBtn);

    expect(
      screen.getAllByText("Stars move because Earth spins west to east.")[0],
    ).toBeInTheDocument();
    expect(
      screen.queryByText("Water plants in the morning."),
    ).not.toBeInTheDocument();
  });

  it("edits raw text preserving prior enrichment and emits analytics on mark reviewed, export, and delete", async () => {
    const mockNote = {
      id: "note-1",
      action_key: "key-1",
      course_id: "course-astronomy",
      lesson_id: "lesson-1",
      slide_title: "Why the sky changes · Slide 2",
      source: "slide" as const,
      raw_text: "Original star notes.",
      status: "enriched" as const,
      reviewed: false,
      reviewed_at: null,
      summary: "AI summary preserved.",
      tags: ["astronomy"],
      links: [],
      version: 1,
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
    };

    vi.spyOn(session, "request").mockImplementation(async (path, method) => {
      if (path === "/v1/notes" && (!method || method === "GET")) {
        return [mockNote];
      }
      if (path === "/v1/notes/note-1" && method === "PATCH") {
        return {
          ...mockNote,
          raw_text: "Updated star notes.",
          version: 2,
        };
      }
      if (path === "/v1/notes/note-1/review" && method === "POST") {
        return {
          ...mockNote,
          reviewed: true,
          reviewed_at: new Date().toISOString(),
        };
      }
      if (path === "/v1/notes/note-1" && method === "DELETE") {
        return { success: true };
      }
      return null;
    });

    render(<NotesPage />);

    expect(
      (await screen.findAllByText("Original star notes."))[0],
    ).toBeInTheDocument();

    // Edit raw text
    const editBtn = screen.getByRole("button", { name: /Edit/i });
    fireEvent.click(editBtn);

    const textarea = screen.getByRole("textbox", { name: /Edit note text/i });
    fireEvent.change(textarea, { target: { value: "Updated star notes." } });

    const saveBtn = screen.getByRole("button", { name: /Save changes/i });
    fireEvent.click(saveBtn);

    await waitFor(() => {
      expect(screen.getByText("Updated star notes.")).toBeInTheDocument();
    });
    // AI additions should remain visible and intact
    expect(screen.getByText("AI additions")).toBeInTheDocument();
    expect(screen.getByText(/AI summary preserved/i)).toBeInTheDocument();

    // Mark reviewed
    const reviewBtn = screen.getByRole("button", { name: /Mark reviewed/i });
    fireEvent.click(reviewBtn);

    await waitFor(() => {
      expect(posthog.capture).toHaveBeenCalledWith("quick_note_reviewed", {
        distinct_id: learnerId,
        platform: "web",
        app_version: "0.1.0",
        source: "client",
        note_id: "note-1",
      });
    });

    // Export Markdown
    const exportBtn = screen.getByRole("button", {
      name: /Export Markdown|Export/i,
    });
    fireEvent.click(exportBtn);

    await waitFor(() => {
      expect(posthog.capture).toHaveBeenCalledWith("ui_control_activated", {
        distinct_id: learnerId,
        platform: "web",
        app_version: "0.1.0",
        source: "client",
        surface: "quick_notes",
        control_id: "export_notes",
      });
    });

    // Delete note
    const deleteBtn = screen.getByRole("button", { name: /Delete/i });
    fireEvent.click(deleteBtn);

    await waitFor(() => {
      expect(posthog.capture).toHaveBeenCalledWith("quick_note_deleted", {
        distinct_id: learnerId,
        platform: "web",
        app_version: "0.1.0",
        source: "client",
        note_id: "note-1",
      });
    });
  });

  it("handles revision conflict when saving edits concurrently and displays conflict resolution options", async () => {
    const mockNote = {
      id: "note-conflict",
      action_key: "key-conflict",
      source: "slide" as const,
      raw_text: "Seasons = tilt, not distance.",
      status: "raw_only" as const,
      reviewed: false,
      version: 1,
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
    };

    const serverConflictNote = {
      ...mockNote,
      raw_text: "Seasons = tilt, not distance. Remote edit from laptop.",
      version: 2,
    };

    vi.spyOn(session, "request").mockImplementation(async (path, method) => {
      if (path === "/v1/notes" && (!method || method === "GET")) {
        return [mockNote];
      }
      if (path === "/v1/notes/note-conflict" && method === "PATCH") {
        const error = Object.assign(new Error("Conflict"), {
          status: 409,
          serverNote: serverConflictNote,
        });
        throw error;
      }
      return null;
    });

    render(<NotesPage />);

    expect(
      (await screen.findAllByText("Seasons = tilt, not distance."))[0],
    ).toBeInTheDocument();

    const editBtn = screen.getByRole("button", { name: /Edit/i });
    fireEvent.click(editBtn);

    const textarea = screen.getByRole("textbox", { name: /Edit note text/i });
    fireEvent.change(textarea, {
      target: {
        value:
          "Seasons = tilt, not distance. Summer when my hemisphere leans in.",
      },
    });

    const saveBtn = screen.getByRole("button", { name: /Save changes/i });
    fireEvent.click(saveBtn);

    // Conflict banner should appear
    expect(
      await screen.findByText("This note changed on another device"),
    ).toBeInTheDocument();
    expect(
      screen.getByText(
        "Pick which version to keep. Nothing is lost until you choose.",
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /Keep selected/i }),
    ).toBeInTheDocument();
  });
});
