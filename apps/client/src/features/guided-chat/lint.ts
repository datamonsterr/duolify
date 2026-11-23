import { apiLimits } from "./limits";
import { exprRefs } from "./expr";
import schema from "./script.schema.json";
import { secureWidgets } from "./types";
import type { Expr, GuidedScript, Validate } from "./types";
import Ajv from "ajv";

export type Catalogue = Record<string, unknown>;

const ajv = new Ajv({ allErrors: true });
const validateShape = ajv.compile(schema);

function maxOf(rule: Validate | undefined) {
  if (!rule) return undefined;
  if (rule.type === "string") return rule.max;
  if (rule.type === "number") return rule.max;
  if (rule.type === "multi") return rule.maxItems;
  if (rule.type === "code") return rule.length;
  return undefined;
}

/** Build-time checks for a script. Returns human-readable problems; empty means valid. */
export function lintScript(
  script: GuidedScript,
  catalogues: { en: Catalogue; ar: Catalogue },
): string[] {
  const problems: string[] = [];
  if (!validateShape(script)) {
    for (const e of validateShape.errors ?? [])
      problems.push(`schema ${e.instancePath || "/"} ${e.message}`);
    return problems;
  }
  const ids = script.steps.map((s) => s.id);
  const dup = ids.find((id, i) => ids.indexOf(id) !== i);
  if (dup) problems.push(`duplicate step id ${dup}`);
  const needKey = (key: string) => {
    for (const [locale, catalogue] of Object.entries(catalogues))
      if (!(key in catalogue)) problems.push(`missing ${locale} key ${key}`);
  };
  const needTarget = (from: string, id: string) => {
    if (!ids.includes(id))
      problems.push(`${from} refers to unknown step ${id}`);
  };
  const checkExpr = (from: string, expr: Expr) =>
    exprRefs(expr).forEach((id) => needTarget(from, id));
  script.steps.forEach((step, index) => {
    needKey(step.promptKey);
    step.options?.forEach((o) => needKey(o.labelKey));
    if (step.errorKey) needKey(step.errorKey);
    if (step.when) checkExpr(step.id, step.when);
    for (const rule of step.next ?? []) {
      checkExpr(step.id, rule.when);
      needTarget(step.id, rule.to);
      if (ids.indexOf(rule.to) <= index)
        problems.push(`${step.id} jumps back or to itself (${rule.to})`);
    }
    step.summaryOf?.forEach((id) => needTarget(step.id, id));
    if (
      (step.widget === "single" || step.widget === "multi") &&
      !step.options?.length
    )
      problems.push(`${step.id} needs options`);
    if (
      step.widget === "confirm" &&
      !step.submit &&
      index === script.steps.length - 1
    )
      problems.push(`${step.id} is the last step and names no submit endpoint`);
    if (
      (secureWidgets.includes(step.widget) || step.destructive) &&
      step.persist
    )
      problems.push(`${step.id} is a secure step and must not persist`);
    if (step.limit) {
      const limit = apiLimits[step.limit];
      if (!limit) problems.push(`${step.id} names unknown limit ${step.limit}`);
      else {
        const max = maxOf(step.validate);
        if (limit.max !== undefined && (max === undefined || max > limit.max))
          problems.push(`${step.id} validator is looser than the API limit`);
        if (
          step.validate?.type === "number" &&
          limit.min !== undefined &&
          step.validate.min < limit.min
        )
          problems.push(`${step.id} minimum is looser than the API limit`);
      }
    }
    if (["email", "code", "password"].includes(step.widget) && !step.input)
      problems.push(`${step.id} needs input name and autocomplete`);
  });
  const last = script.steps[script.steps.length - 1];
  if (last.widget !== "confirm" || !last.submit)
    problems.push("the last step must be a confirm card with a submit");
  return problems;
}
