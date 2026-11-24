import { compileV2 } from "./rig-v2";
import { writeFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import { defaultDraft, changeSpecies } from "./draft";
import { getDefaultCastFixtures } from "./fixtures";
import { generateMotionSlideHtml, POSE_FOR_EXPRESSION } from "./motion-slide";
import type { LessonCastSnapshot, SlideManifest } from "./types";

const [guide, supporter] = getDefaultCastFixtures();
const robot = defaultDraft("robot");
robot.info.name = "Bolt";
const fox = changeSpecies(defaultDraft("animal"), "fox");
fox.info.name = "Ember";

function cast(withDocs: boolean): LessonCastSnapshot {
  return {
    cast_source: "custom",
    schema_version: 1,
    rig_version: "original-v1",
    voice_config_version: "v1",
    speakers: {
      guide: {
        speaker_key: "guide",
        role: "guide",
        character_id: "c1",
        character_version: 1,
        spec: { ...guide!, info: { ...guide!.info, name: "Bolt" } },
        ...(withDocs ? { document: robot } : {}),
      },
      supporter: {
        speaker_key: "supporter",
        role: "supporter",
        character_id: "c2",
        character_version: 1,
        spec: { ...supporter!, info: { ...supporter!.info, name: "Ember" } },
        ...(withDocs ? { document: fox } : {}),
      },
    },
  };
}
const slide = (): SlideManifest =>
  ({
    slide_id: "slide-1",
    duration_ms: 6000,
    segments: [
      {
        speaker_key: "guide",
        speaker_label: "Bolt",
        text: "Let us count together.",
        expression: "happy",
        start_ms: 0,
        duration_ms: 3000,
      },
      {
        speaker_key: "supporter",
        speaker_label: "Ember",
        text: "One, two, three!",
        expression: "celebrate",
        start_ms: 3000,
        duration_ms: 3000,
      },
    ],
  }) as unknown as SlideManifest;

describe("kind-aware cast rendering", () => {
  it("renders pinned v2 documents with derived poses; v1 casts are unchanged", () => {
    const html = generateMotionSlideHtml(slide(), cast(true), { compileV2 });
    expect(html).toMatch(/data-pose=\\*"talk/);
    expect(html).toMatch(/data-pose=\\*"celebrate/);
    const v1 = generateMotionSlideHtml(slide(), cast(false));
    expect(v1).not.toContain("data-pose=");
    expect(POSE_FOR_EXPRESSION.sad).toBe("shrug");
    if (process.env.SLIDE_HTML_OUT)
      writeFileSync(process.env.SLIDE_HTML_OUT, html);
  });
});
