import type { SlideSegment } from "./types";

/** The stable id of a segment's narration row; the API names it `<slide>__s<position>`. */
export const segmentSlideId = (slideId: string, index: number) =>
  `${slideId}__s${index}`;

export type SegmentPlay = {
  index: number;
  segment: SlideSegment;
  audio_url: string;
  alignment_url: string;
  duration_ms: number;
  /** Where this segment's audio starts on the slide's narration timeline. */
  offset_ms: number;
};

type SegmentedSlide = {
  segments?: SlideSegment[];
};

/** Segments with ready audio, in their original speaker order, on one continuous timeline. */
export function playableSegments(slide: SegmentedSlide): SegmentPlay[] {
  const out: SegmentPlay[] = [];
  let offset = 0;
  (slide.segments ?? []).forEach((segment, index) => {
    const n = segment.narration;
    if (n?.status !== "ready" || !n.audio_url || !n.alignment_url) return;
    const duration = n.duration_ms ?? 0;
    out.push({
      index,
      segment,
      audio_url: n.audio_url,
      alignment_url: n.alignment_url,
      duration_ms: duration,
      offset_ms: offset,
    });
    offset += duration;
  });
  return out;
}

/** Total narrated length; the slide holds its final frame until the last segment ends. */
export function narratedDuration(slide: SegmentedSlide): number {
  const plays = playableSegments(slide);
  const last = plays[plays.length - 1];
  return last ? last.offset_ms + last.duration_ms : 0;
}

/** The segment that covers a slide time and how far into its audio that time is. */
export function locateSegment(
  plays: SegmentPlay[],
  timeMs: number,
): { play: SegmentPlay; within_ms: number } | null {
  if (plays.length === 0) return null;
  const t = Math.max(0, timeMs);
  for (const play of plays) {
    if (t < play.offset_ms + play.duration_ms)
      return { play, within_ms: Math.max(0, t - play.offset_ms) };
  }
  const last = plays[plays.length - 1];
  return { play: last, within_ms: last.duration_ms };
}

/** True when the slide has pinned-speaker narration rows (ready or not). */
export const hasSegmentNarration = (slide: SegmentedSlide): boolean =>
  (slide.segments ?? []).some((s) => s.narration !== undefined);

/** Segments whose audio failed: captions stay readable, Retry narration is offered. */
export const failedSegmentIndexes = (slide: SegmentedSlide): number[] =>
  (slide.segments ?? []).flatMap((s, i) =>
    s.narration?.status === "failed" ? [i] : [],
  );

export const pendingSegments = (slide: SegmentedSlide): boolean =>
  (slide.segments ?? []).some((s) => s.narration?.status === "pending");

/** Insufficient audio balance or narration off: only audio is disabled. */
export const pausedForCredits = (slide: SegmentedSlide): boolean =>
  (slide.segments ?? []).some(
    (s) =>
      s.narration?.status === "disabled" &&
      s.narration.reason === "insufficient_credits",
  );
