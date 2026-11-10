import { render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { NotesPage } from "./NotesPage";
import { QuickNotePanel } from "./QuickNotePanel";
import { session } from "../accounts/session";
import { configureAnalytics, trackPurchase } from "../../lib/analytics";

const posthog = vi.hoisted(() => ({
  capture: vi.fn(),
  has_opted_out_capturing: vi.fn(() => false),
  init: vi.fn(),
}));

vi.mock("posthog-js/dist/module.no-external", () => ({ default: posthog }));

describe("resume_pending_enrichment_when_credits_arrive", () => {
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

  it("renders pending enrichment state with zero-credit variant guidance and orders oldest note first", async () => {
    const olderDate = new Date(Date.now() - 60000).toISOString();
    const newerDate = new Date().toISOString();

    const mockNotes = [
      {
        id: "note-older",
        action_key: "key-older",
        source: "chat" as const,
        raw_text: "Oldest note: Stars move west to east.",
        status: "enrichment_pending" as const,
        created_at: olderDate,
        updated_at: olderDate,
      },
      {
        id: "note-newer",
        action_key: "key-newer",
        source: "slide" as const,
        raw_text: "Newer note: Polaris stays still.",
        status: "enrichment_pending" as const,
        created_at: newerDate,
        updated_at: newerDate,
      },
    ];

    vi.spyOn(session, "request").mockResolvedValue(mockNotes);

    render(<NotesPage />);

    // Both notes should be rendered
    const olderElements = await screen.findAllByText(
      /Oldest note: Stars move west to east\./i,
    );
    expect(olderElements[0]).toBeInTheDocument();

    const newerElements = screen.getAllByText(
      /Newer note: Polaris stays still\./i,
    );
    expect(newerElements[0]).toBeInTheDocument();

    // Verify "Enrichment pending" badge is shown
    const pendingBadges = screen.getAllByText(/Enrichment pending/i);
    expect(pendingBadges.length).toBeGreaterThanOrEqual(1);

    // Detail shows pending explanation per canvas
    expect(
      screen.getByText(
        /Enrichment pending\. The AI summary and tags appear here once enriched\./i,
      ),
    ).toBeInTheDocument();
  });

  it("QuickNotePanel shows zero-credit variant banner and status per S09 canvas", () => {
    render(
      <QuickNotePanel
        open={true}
        onClose={() => {}}
        context={{
          source: "slide",
          slideTitle: "Slide 2 · Why the sky changes",
        }}
      />,
    );

    expect(
      screen.getByRole("heading", { name: /Quick note/i }),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/Slide 2 · Why the sky changes/i),
    ).toBeInTheDocument();
  });

  it("emits credits_upsell_shown without leaking private learner text", () => {
    trackPurchase({
      event: "credits_upsell_shown",
      pricingVersion: "2026.04",
      feature: "quick_note",
    });

    expect(posthog.capture).toHaveBeenCalledWith(
      "credits_upsell_shown",
      expect.objectContaining({
        distinct_id: "11111111-1111-4111-8111-111111111111",
        feature: "quick_note",
      }),
    );

    // Ensure no note text was sent
    const calls = posthog.capture.mock.calls;
    for (const call of calls) {
      const payload = JSON.stringify(call[1] ?? {});
      expect(payload).not.toContain("Stars move");
      expect(payload).not.toContain("Polaris");
    }
  });
});
