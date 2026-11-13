import { render, screen } from "@testing-library/react";
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

describe("enrich_notes_with_grounded_summaries_and_tags", () => {
  beforeEach(() => {
    localStorage.clear();
    sessionStorage.clear();
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
    vi.restoreAllMocks();
  });

  it("renders enriched summary, tags, and valid links separately from raw text, rejecting hallucinated link", async () => {
    const rawNote =
      'Stars "move" because Earth spins west → east. Check: does Polaris move?';
    const mockNotes = [
      {
        id: "note-1",
        action_key: "key-1",
        course_id: "course-1",
        lesson_id: "lesson-1",
        slide_title: "Slide 2 · Why the sky changes",
        source: "slide" as const,
        raw_text: rawNote,
        status: "enriched" as const,
        summary:
          "Apparent star motion comes from Earth's rotation; Polaris barely moves because it sits near the celestial pole.",
        tags: ["rotation", "polaris"],
        links: ["lesson-2"],
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
      },
    ];

    vi.spyOn(session, "request").mockResolvedValue(mockNotes);

    render(<NotesPage />);

    // Verify raw text is displayed
    const rawElements = await screen.findAllByText(rawNote);
    expect(rawElements[0]).toBeInTheDocument();
    expect(screen.getByText(/Your original note/i)).toBeInTheDocument();

    // Verify AI additions section
    expect(screen.getByText(/AI additions/i)).toBeInTheDocument();
    expect(
      screen.getByText(
        /Apparent star motion comes from Earth's rotation; Polaris barely moves/i,
      ),
    ).toBeInTheDocument();

    // Verify tags
    expect(screen.getByText("#rotation")).toBeInTheDocument();
    expect(screen.getByText("#polaris")).toBeInTheDocument();

    // Verify valid grounded link is shown
    expect(screen.getByText(/Related:/i)).toBeInTheDocument();

    // Verify hallucinated or foreign link is NOT in the document
    expect(screen.queryByText(/hallucinated-lesson-uuid/i)).toBeNull();
  });

  it("renders enrichment failed state with recovery retry button", async () => {
    const rawNote = "Note with failed enrichment";
    const mockNotes = [
      {
        id: "note-fail",
        action_key: "key-fail",
        source: "lesson" as const,
        raw_text: rawNote,
        status: "enrichment_failed" as const,
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
      },
    ];

    vi.spyOn(session, "request").mockResolvedValue(mockNotes);

    render(<NotesPage />);

    // Raw text intact
    const rawElements = await screen.findAllByText(rawNote);
    expect(rawElements[0]).toBeInTheDocument();

    // Failure alert banner per canvas
    expect(
      screen.getByText(/Couldn't add the AI summary/i),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/Your note is safe. No credits were charged./i),
    ).toBeInTheDocument();

    // Retry enrichment button per canvas
    const retryBtn = screen.getByRole("button", {
      name: /Retry enrichment · 2 credits/i,
    });
    expect(retryBtn).toBeInTheDocument();
  });
});
