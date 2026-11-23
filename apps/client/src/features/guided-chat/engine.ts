import { evalExpr } from "./expr";
import { maxDraftBytes } from "./limits";
import { secureWidgets } from "./types";
import type { Answer, Answers, GuidedScript, Step, Value } from "./types";
import { validateStep } from "./validate";
import type { Invalid } from "./validate";

export type GuidedState = {
  scriptId: string;
  version: number;
  answers: Answers;
  /** Step being asked or edited; null when every reachable step is answered. */
  editing: string | null;
};

export type Walk = {
  /** Answered steps on the reachable route, in order. */
  path: string[];
  /** First reachable step without an answer. */
  frontier: string | null;
};

export const stepById = (script: GuidedScript, id: string) =>
  script.steps.find((s) => s.id === id);

/** Follows `when` and `next` over the current answers. Unanswered steps are passed over so that
 * answers after a gap stay reachable; the frontier is the first gap. */
export function walk(script: GuidedScript, answers: Answers): Walk {
  const path: string[] = [];
  let frontier: string | null = null;
  let index = 0;
  while (index < script.steps.length) {
    const step = script.steps[index];
    if (step.when && !evalExpr(step.when, answers)) {
      index += 1;
      continue;
    }
    if (answers[step.id]) path.push(step.id);
    else frontier ??= step.id;
    const jump = step.next?.find((rule) => evalExpr(rule.when, answers));
    const to = jump && script.steps.findIndex((s) => s.id === jump.to);
    index = to !== undefined && to > index ? to : index + 1;
  }
  return { path, frontier };
}

export function start(
  script: GuidedScript,
  answers: Answers = {},
): GuidedState {
  return {
    scriptId: script.id,
    version: script.version,
    answers,
    editing: null,
  };
}

export const current = (script: GuidedScript, state: GuidedState) =>
  state.editing ?? walk(script, state.answers).frontier;

export type Outcome = {
  state: GuidedState;
  error?: Invalid;
  /** Answers removed because the edit made them unreachable or invalid. */
  dropped: string[];
};

function settle(script: GuidedScript, answers: Answers) {
  const kept: Answers = { ...answers };
  const dropped: string[] = [];
  for (let guard = 0; guard <= script.steps.length; guard += 1) {
    const { path } = walk(script, kept);
    const stale = Object.keys(kept).filter(
      (id) =>
        !path.includes(id) ||
        (!kept[id].skipped &&
          validateStep(stepById(script, id)!, kept[id].value) !== null) ||
        (kept[id].skipped && !stepById(script, id)?.skippable),
    );
    if (!stale.length) break;
    for (const id of stale) {
      delete kept[id];
      dropped.push(id);
    }
  }
  return { kept, dropped };
}

// Any edit invalidates a confirm card: the Learner must confirm the new summary.
function withoutConfirms(script: GuidedScript, answers: Answers): Answers {
  const out = { ...answers };
  for (const step of script.steps)
    if (step.widget === "confirm") delete out[step.id];
  return out;
}

/** Answer the step in focus, or replace an earlier answer. Never submits anything. */
export function answer(
  script: GuidedScript,
  state: GuidedState,
  stepId: string,
  value: Value,
): Outcome {
  const step = stepById(script, stepId);
  if (!step) return { state, dropped: [] };
  const error = validateStep(step, value);
  if (error) return { state, error, dropped: [] };
  const answers = {
    ...withoutConfirms(script, state.answers),
    [stepId]: { value } as Answer,
  };
  // The edited answer itself is trusted; later answers are re-checked against the new branches.
  const { kept, dropped } = settle(script, answers);
  return {
    state: { ...state, answers: kept, editing: null },
    dropped: dropped.filter((id) => id !== stepId),
  };
}

export function skip(
  script: GuidedScript,
  state: GuidedState,
  stepId: string,
): Outcome {
  const step = stepById(script, stepId);
  if (!step?.skippable) return { state, error: "invalid", dropped: [] };
  const answers = {
    ...withoutConfirms(script, state.answers),
    [stepId]: { value: null, skipped: true },
  };
  const { kept, dropped } = settle(script, answers);
  return {
    state: { ...state, answers: kept, editing: null },
    dropped: dropped.filter((id) => id !== stepId),
  };
}

export function edit(state: GuidedState, stepId: string): GuidedState {
  return state.answers[stepId] ? { ...state, editing: stepId } : state;
}
export const cancelEdit = (state: GuidedState): GuidedState => ({
  ...state,
  editing: null,
});

/** "Back": reopen the most recent answered step. */
export function back(script: GuidedScript, state: GuidedState): GuidedState {
  const { path } = walk(script, state.answers);
  const last = path[path.length - 1];
  return last ? edit(state, last) : state;
}

export const restart = (script: GuidedScript): GuidedState => start(script);

export function isComplete(script: GuidedScript, state: GuidedState) {
  return walk(script, state.answers).frontier === null;
}

export const isSecure = (step: Step) =>
  secureWidgets.includes(step.widget) || Boolean(step.destructive);

/** The part of the answers that may leave the device: persistent steps only. */
export function draftAnswers(script: GuidedScript, answers: Answers): Answers {
  const out: Answers = {};
  for (const step of script.steps) {
    const a = answers[step.id];
    // A skip carries no content, so it may be saved even when the step's answer would not be.
    if (a && (step.persist || a.skipped) && !isSecure(step)) out[step.id] = a;
  }
  return out;
}

export type Draft = {
  script_version: number;
  step_id: string;
  answers: Answers;
  updated_at?: string;
};

export function toDraft(
  script: GuidedScript,
  state: GuidedState,
): Draft | null {
  const answers = draftAnswers(script, state.answers);
  const draft: Draft = {
    script_version: script.version,
    step_id: current(script, state) ?? "",
    answers,
  };
  return new TextEncoder().encode(JSON.stringify(draft)).length > maxDraftBytes
    ? null
    : draft;
}

export type Restored = {
  state: GuidedState;
  notice: "migrated" | "restarted" | null;
};

/** Resume from a draft. A different script version keeps only answers that still fit. */
export function restore(script: GuidedScript, draft: Draft | null): Restored {
  if (!draft) return { state: start(script), notice: null };
  const sameVersion = draft.script_version === script.version;
  const answers: Answers = {};
  for (const [id, a] of Object.entries(draft.answers)) {
    const step = stepById(script, id);
    if (!step || isSecure(step) || !(step.persist || a.skipped)) continue;
    if (a.skipped ? step.skippable : validateStep(step, a.value) === null)
      answers[id] = a;
  }
  const { kept } = settle(script, answers);
  if (sameVersion) return { state: start(script, kept), notice: null };
  const any = Object.keys(kept).length > 0;
  return {
    state: start(script, kept),
    notice: any ? "migrated" : "restarted",
  };
}
