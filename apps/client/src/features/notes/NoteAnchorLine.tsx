import { AppLink } from "../../lib/navigation";

/** The anchor of a saved Quick note as the API returns it. */
export type NoteAnchorView = {
  playback_ms: number;
  rect?: { x: number; y: number; w: number; h: number } | null;
  input: "typed" | "voice";
  /** `unavailable` once the Lesson was removed: the note stays readable on its own. */
  source_state: "available" | "unavailable";
};

export const formatAnchorTime = (ms: number) => {
  const s = Math.floor(Math.max(0, ms) / 1000);
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`;
};

/** Where "Show in Lesson" goes: that Slide, that time, the region outlined. */
export function showInLessonHref(
  lessonId: string,
  slideId: number,
  anchor: Pick<NoteAnchorView, "playback_ms" | "rect">,
): string {
  const q = new URLSearchParams({
    slide: String(slideId),
    t: String(Math.round(anchor.playback_ms)),
  });
  if (anchor.rect) {
    const { x, y, w, h } = anchor.rect;
    q.set("region", [x, y, w, h].map((n) => n.toFixed(4)).join(","));
  }
  q.set("returnTo", "/notes");
  return `/lesson/${lessonId}?${q.toString()}`;
}

export function NoteAnchorLine({
  anchor,
  lessonId,
  slideId,
}: {
  anchor: NoteAnchorView;
  lessonId?: string | null;
  slideId?: number | null;
}) {
  const unavailable =
    anchor.source_state === "unavailable" ||
    !lessonId ||
    slideId === null ||
    slideId === undefined;
  if (unavailable) {
    return (
      <span
        data-testid="note-source-unavailable"
        style={{ fontSize: 13, fontWeight: 700, color: "#5E5E5E" }}
      >
        Source unavailable · this Lesson was removed. Your note is kept.
      </span>
    );
  }
  return (
    <span
      style={{
        display: "flex",
        gap: 10,
        alignItems: "center",
        flexWrap: "wrap",
        fontSize: 13,
        fontWeight: 700,
      }}
    >
      <span data-testid="note-anchor-label">
        Slide {slideId + 1} · {formatAnchorTime(anchor.playback_ms)}
        {anchor.rect ? " · area selected" : ""}
        {anchor.input === "voice" ? " · dictated" : ""}
      </span>
      <AppLink
        href={showInLessonHref(lessonId, slideId, anchor)}
        data-testid="note-show-in-lesson"
        style={{ color: "#1667AB", fontWeight: 800 }}
      >
        Show in Lesson
      </AppLink>
    </span>
  );
}
