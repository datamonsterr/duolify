import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { NotesPage } from "./NotesPage";
import { session } from "../accounts/session";

const at = new Date().toISOString();
const note = (over: Record<string, unknown>) => ({
  id: "n1",
  action_key: "k1",
  source: "slide" as const,
  raw_text: "an unbuffered chanel blocks",
  status: "enriched" as const,
  created_at: at,
  updated_at: at,
  ...over,
});
const formatted = {
  title: "Unbuffered channels",
  formatted_body: "- An unbuffered channel blocks.",
};

async function open(n: object) {
  vi.spyOn(session, "request").mockResolvedValue([n]);
  render(<NotesPage />);
  fireEvent.click((await screen.findAllByText(/chanel/))[0]);
}

describe("a formatted Voice note and the AI additions card", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.restoreAllMocks();
  });

  it("does not claim enrichment is pending once the note is formatted and enriched", async () => {
    await open(note(formatted));
    expect(await screen.findByTestId("note-formatted")).toBeInTheDocument();
    expect(screen.queryByText(/Enrichment pending/i)).toBeNull();
    expect(screen.queryByText("AI additions")).toBeNull();
  });

  it("shows the summary when a formatted note also has one", async () => {
    await open(note({ ...formatted, summary: "Unbuffered channels block." }));
    expect(await screen.findByText(/Unbuffered channels block\./)).toBeTruthy();
    expect(screen.queryByText(/Enrichment pending/i)).toBeNull();
  });

  it("still says pending while the note waits to be enriched", async () => {
    await open(note({ status: "enrichment_pending" }));
    expect(
      (await screen.findAllByText(/Enrichment pending\. The AI summary/i))
        .length,
    ).toBe(1);
    expect(screen.queryByTestId("note-formatted")).toBeNull();
  });

  it("a raw Voice note saved without formatting says so", async () => {
    await open(note({ status: "raw_only" }));
    expect(
      await screen.findByText(/Raw note saved without AI enrichment/i),
    ).toBeTruthy();
  });
});
