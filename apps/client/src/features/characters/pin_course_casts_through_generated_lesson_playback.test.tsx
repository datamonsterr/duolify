import { expect, it } from "vitest";
import { createDefaultTracerManifest } from "./fixtures";
import { generatedCharacterDocument, activeCharacterSegment } from "./playback";

it("uses pinned versions, every speaker and the host clock; preserves caption recovery", () => {
  const m = createDefaultTracerManifest();
  const slide = m.slides[0];
  expect(activeCharacterSegment(slide.segments, 1800)?.speaker_key).toBe(
    "supporter",
  );
  expect(activeCharacterSegment(slide.segments, 0)?.speaker_key).toBe("guide");
  const before = JSON.stringify(m);
  const html = generatedCharacterDocument(slide, m.cast, true, false);
  expect(html).toContain("event.source !== parent");
  expect(html).toContain('"tick"');
  expect(html).toContain("supporter");
  expect(html).not.toContain("setInterval");
  m.cast.speakers.guide.spec.rig_version = "unsupported" as "original-v1";
  const fallback = generatedCharacterDocument(slide, m.cast, true, true);
  expect(fallback).toContain("unsupported_rig");
  expect(fallback).toContain(slide.segments[0].text);
  expect(JSON.stringify(m)).not.toBe(before);
  const snapshot = JSON.stringify(m);
  generatedCharacterDocument(slide, m.cast, true, true);
  expect(JSON.stringify(m)).toBe(snapshot);
});
