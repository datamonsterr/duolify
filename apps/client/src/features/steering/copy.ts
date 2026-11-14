import type { Instruction, InstructionLimits } from "./api";
export const DEFAULT_LIMITS: InstructionLimits = {
  max_characters: 500,
  max_active: 20,
  max_total_characters: 4000,
  adds_per_hour: 10,
};
const REASONS: Record<string, string> = {
  empty: "Write something first.",
  too_long: "Keep it within the character limit.",
  control_characters: "Remove hidden or control characters.",
  urls_not_allowed: "Links are not allowed. Describe the focus in words.",
  prompt_injection:
    "Instructions can change style and focus, not grading or the rules of the lesson test.",
  unsafe_content: "This instruction can't be used.",
  off_topic_for_course:
    "This doesn't seem related to this course. Edit it and try again.",
  invalid_scope: "Choose Next lesson or Rest of course.",
  total_length_limit:
    "Active instructions would pass the total character limit. Delete or shorten one.",
  invalid_input: "Check the text and scope, then try again.",
  screening_unavailable:
    "Couldn't check right now. Nothing was saved. Try again in a moment.",
  version_conflict:
    "This instruction changed elsewhere. The list was refreshed; your draft is kept.",
  not_editable: "This instruction was already applied, so it can't be edited.",
  not_found: "This instruction no longer exists.",
  rate_limited: "You have added too many instructions this hour. Try later.",
};
export const reasonCopy = (code: string) => REASONS[code] ?? "";
export const countChars = (s: string) => Array.from(s).length;
export const scopeLabel = (scope: string) =>
  scope === "next_lesson" ? "Next lesson" : "Rest of course";
export const statusLabel = (i: Instruction) =>
  i.status === "applied"
    ? `Applied to lesson ${i.applied_lesson_id}`
    : "Active";
export function deliveryHint(i: Instruction) {
  if (i.status !== "active" || i.scope !== "next_lesson") return "";
  if (i.delivery === "running")
    return "Used by the lesson being prepared; applied when it is delivered";
  if (i.delivery === "waits")
    return "Waits for the lesson after the one being prepared";
  if (i.delivery === "ready")
    return "Applies to the next lesson to be prepared";
  return "";
}
const n = (v: number) => v.toLocaleString("en-US");
export const headerCopy = (active: number, chars: number, max: number) =>
  `${active} active · ${n(chars)} / ${n(max)} characters`;
