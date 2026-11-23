import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { api } from "../../lib/api";
import { parseArrival } from "../lesson-player/arrival";
import { NoteAnchorLine, showInLessonHref } from "./NoteAnchorLine";

const anchor = {
  playback_ms: 12_500,
  rect: { x: 0.1, y: 0.2, w: 0.4, h: 0.3 },
  input: "typed" as const,
  source_state: "available" as const,
};

describe("anchored note source line", () => {
  it("offers Show in Lesson with slide, time and region in the link", () => {
    render(<NoteAnchorLine anchor={anchor} lessonId="l1" slideId={2} />);
    expect(screen.getByTestId("note-anchor-label").textContent).toContain(
      "Slide 3 · 0:12 · area selected",
    );
    const href = screen
      .getByTestId("note-show-in-lesson")
      .getAttribute("href")!;
    expect(href).toBe(showInLessonHref("l1", 2, anchor));
    const q = new URL(href, "http://x").searchParams;
    expect(parseArrival(q.get("t")!, q.get("region")!)).toEqual({
      timeMs: 12_500,
      rect: { x: 0.1, y: 0.2, w: 0.4, h: 0.3 },
    });
  });
  it("shows Course › Lesson › Slide and a timestamp chip once the titles load", async () => {
    vi.spyOn(api, "course").mockResolvedValue({
      id: "c1",
      title: "Physics",
      description: "",
      units: [
        {
          title: "U1",
          lessons: [{ id: "l1", title: "Light", state: "done" }],
        },
      ],
    });
    render(
      <NoteAnchorLine
        anchor={{ ...anchor, input: "voice" }}
        courseId="c1"
        lessonId="l1"
        slideId={2}
        slideTitle="Why the sky is blue"
      />,
    );
    expect(
      await screen.findByText("Physics › Light › Why the sky is blue"),
    ).toBeTruthy();
    expect(screen.getByTestId("note-show-in-lesson").textContent).toBe(
      "Show in Lesson · 0:12",
    );
  });
  it("a removed Lesson reads as source unavailable with no link", () => {
    render(
      <NoteAnchorLine
        anchor={{ ...anchor, source_state: "unavailable" }}
        lessonId={null}
        slideId={2}
      />,
    );
    expect(screen.getByTestId("note-source-unavailable")).toBeTruthy();
    expect(screen.queryByTestId("note-show-in-lesson")).toBeNull();
  });
  it("ignores malformed arrival params", () => {
    expect(parseArrival("abc", "1,2")).toBeUndefined();
    expect(parseArrival(undefined, "0.5,0.5,0.9,0.9")).toBeUndefined();
    expect(parseArrival("500", undefined)).toEqual({
      timeMs: 500,
      rect: undefined,
    });
  });
});
