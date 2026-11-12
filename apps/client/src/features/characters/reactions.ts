import type { Expression } from "./types";

export type ReactionTrigger =
  "answer_correct" | "answer_incorrect" | "test_passed";

export interface Reaction {
  trigger: ReactionTrigger | "neutral";
  expression: Expression;
  copy: string;
}

/** Approved supportive lines. Mirrors the API and engine lists. */
export const APPROVED_REACTION_COPY: Record<ReactionTrigger, string> = {
  answer_correct: "Nice work — that's right.",
  test_passed: "You passed! Great work.",
  answer_incorrect: "Good try — read the explanation, then try the next one.",
};

const ALLOWED: Record<ReactionTrigger, Expression[]> = {
  answer_correct: ["happy"],
  test_passed: ["celebrate"],
  answer_incorrect: ["thinking", "sad"],
};

/** Static neutral reaction used when the API sent none or an invalid one. */
export function neutralReaction(): Reaction {
  return { trigger: "neutral", expression: "idle", copy: "" };
}

/**
 * Validates an API reaction against the closed set. Free text is never shown:
 * the copy is always the approved line for the trigger.
 */
export function parseReaction(raw: unknown): Reaction | null {
  if (!raw || typeof raw !== "object") return null;
  const { trigger, expression } = raw as Record<string, unknown>;
  if (typeof trigger !== "string" || !(trigger in ALLOWED)) return null;
  const t = trigger as ReactionTrigger;
  if (!ALLOWED[t].includes(expression as Expression)) return null;
  return {
    trigger: t,
    expression: expression as Expression,
    copy: APPROVED_REACTION_COPY[t],
  };
}
