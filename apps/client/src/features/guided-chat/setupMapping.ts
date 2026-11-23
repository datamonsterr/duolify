import type { ContextFields } from "../accounts/learner-context";
import type { Draft } from "./engine";
import type { Answers, Value } from "./types";

/** Existing profile values become answered bubbles so "update by chat" opens prefilled. */
export function profileToDraft(profile: ContextFields): Draft {
  const answers: Answers = {};
  if (profile.goal) answers.goal = { value: profile.goal };
  if (profile.level) answers.level = { value: profile.level };
  if (profile.daily_minutes)
    answers.daily_minutes = { value: profile.daily_minutes };
  if (profile.reminder_enabled !== null) {
    answers.reminder_on = { value: profile.reminder_enabled };
    if (profile.reminder_enabled && profile.reminder_time)
      answers.reminder_time = { value: profile.reminder_time };
  }
  return { script_version: 1, step_id: "", answers };
}

/** Chat answers to the existing profile fields. Skipped steps are left out. */
export function answersToFields(
  answers: Record<string, Value>,
  label: (key: string) => string,
  goalOptions: string[],
): Partial<ContextFields> {
  const fields: Partial<ContextFields> = {};
  const goal = answers.goal;
  if (typeof goal === "string" && goal)
    fields.goal = goalOptions.includes(goal)
      ? label(`g.setup.goal.${goal}`)
      : goal;
  if (typeof answers.level === "string") fields.level = answers.level;
  if (typeof answers.daily_minutes === "number")
    fields.daily_minutes = answers.daily_minutes;
  if (typeof answers.reminder_on === "boolean") {
    fields.reminder_enabled = answers.reminder_on;
    if (answers.reminder_on && typeof answers.reminder_time === "string")
      fields.reminder_time = answers.reminder_time;
  }
  return fields;
}
