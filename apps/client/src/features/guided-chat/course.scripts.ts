import "./messages.brief";
import "./messages.course";
import type { GuidedScript } from "./types";

const o = (prefix: string, ids: string[]) =>
  ids.map((id) => ({ id, labelKey: `${prefix}.${id}` }));

/** Per-course level override. Tapping a level is the whole answer; confirm saves it. */
export const courseLevel: GuidedScript = {
  id: "course.level",
  version: 1,
  entryPoints: ["profile"],
  flag: "FEATURE_GUIDED_COURSE_LEVEL",
  steps: [
    {
      id: "level",
      widget: "single",
      promptKey: "g.level.q",
      options: o("g.brief.level", ["beginner", "intermediate", "advanced"]),
      validate: { type: "enum" },
      persist: true,
      analyticsId: "level",
    },
    {
      id: "confirm",
      widget: "confirm",
      promptKey: "g.level.confirm.q",
      summaryOf: ["level"],
      persist: false,
      submit: "POST /v1/courses/{id}/level",
      analyticsId: "confirm",
    },
  ],
};

/**
 * Course steering: scope, instruction, confirm. The server still screens the text; any
 * outline change proposal stays an AI result with its own quote and is not part of this script.
 */
export const courseSteer: GuidedScript = {
  id: "course.steer",
  version: 1,
  entryPoints: ["course_steer"],
  flag: "FEATURE_GUIDED_COURSE_STEER",
  steps: [
    {
      id: "scope",
      widget: "single",
      promptKey: "g.steer.scope.q",
      options: o("g.steer.scope", ["next_lesson", "rest_of_course"]),
      validate: { type: "enum" },
      persist: false,
      analyticsId: "scope",
    },
    {
      id: "text",
      widget: "longtext",
      promptKey: "g.steer.text.q",
      validate: { type: "string", max: 500 },
      limit: "steer.text",
      persist: false,
      analyticsId: "text",
    },
    {
      id: "confirm",
      widget: "confirm",
      promptKey: "g.steer.confirm.q",
      summaryOf: ["scope", "text"],
      persist: false,
      submit: "POST /v1/courses/{id}/instructions",
      analyticsId: "confirm",
    },
  ],
};
