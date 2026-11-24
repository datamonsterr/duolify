import type { Brief } from "../courses/brief";
import type { Draft } from "./engine";
import type { Answers, Value } from "./types";

// Brief values the existing endpoint and engine already understand, by option id.
const levels: Record<string, string> = {
  beginner: "Beginner",
  intermediate: "Intermediate",
  advanced: "Advanced",
};
const maths: Record<string, string> = {
  none: "No maths",
  light: "Light maths",
  full: "Full maths",
};
const where: Record<string, string> = {
  home: "At home",
  commute: "On the go",
  class: "In class",
};
const length: Record<string, string> = {
  short: "5 minutes",
  medium: "10 minutes",
  long: "15 minutes",
};
const language: Record<string, string> = { en: "English", ar: "Arabic" };
const tables: Record<string, Record<string, string>> = {
  level: levels,
  maths,
  viewing_conditions: where,
  lesson_length: length,
  language,
};

const reverse = (table: Record<string, string>, value: string) =>
  Object.entries(table).find(
    ([, v]) => v.toLowerCase() === value.toLowerCase(),
  )?.[0];

/** A seeded or half-filled brief becomes answered bubbles; free-form values that are not
 * options are dropped for option steps (they are re-asked). */
export function briefToDraft(brief: Brief): Draft {
  const answers: Answers = {};
  for (const [id, raw] of Object.entries(brief)) {
    if (!raw) continue;
    const table = tables[id];
    if (!table) answers[id] = { value: raw.slice(0, 500) };
    else {
      const option = reverse(table, raw);
      if (option) answers[id] = { value: option };
    }
  }
  return { script_version: 1, step_id: "", answers };
}

export function answersToBrief(
  answers: Record<string, Value>,
  base: Brief,
): Brief {
  const brief = { ...base };
  for (const key of Object.keys(brief) as (keyof Brief)[]) {
    const v = answers[key];
    if (typeof v !== "string") {
      brief[key] = "";
      continue;
    }
    brief[key] = tables[key]?.[v] ?? v;
  }
  return brief;
}
