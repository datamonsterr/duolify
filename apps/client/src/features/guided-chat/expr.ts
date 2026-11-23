import type { Answers, Expr, Value } from "./types";

const same = (a: Value | undefined, b: Value) =>
  JSON.stringify(a ?? null) === JSON.stringify(b);

export function isAnswered(answers: Answers, id: string) {
  const a = answers[id];
  return Boolean(a && !a.skipped && a.value !== null && a.value !== "");
}

export function evalExpr(expr: Expr, answers: Answers): boolean {
  if ("eq" in expr) return same(answers[expr.eq[0]]?.value, expr.eq[1]);
  if ("in" in expr) {
    const value = answers[expr.in[0]]?.value;
    return expr.in[1].some((candidate) => same(value, candidate));
  }
  if ("answered" in expr) return isAnswered(answers, expr.answered);
  if ("not" in expr) return !evalExpr(expr.not, answers);
  return false;
}

/** Step ids an expression reads; used by the script lint. */
export function exprRefs(expr: Expr): string[] {
  if ("eq" in expr) return [expr.eq[0]];
  if ("in" in expr) return [expr.in[0]];
  if ("answered" in expr) return [expr.answered];
  return exprRefs(expr.not);
}
