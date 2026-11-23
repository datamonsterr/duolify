import partsJson from "../../../../../services/api/internal/characters/rig/parts.v2.json";
import {
  EXPRESSIONS_V2,
  type CharacterSpecV2,
  type ExpressionV2,
  type Pose,
} from "./types-v2";

export type RigView = "avatar" | "body";
export const POSES: Pose[] = [
  "idle",
  "talk",
  "point",
  "think",
  "celebrate",
  "wave",
  "shrug",
];

type Node = string | Node[] | Record<string, unknown> | null | undefined;
interface Library {
  views: Record<string, { viewBox: string; budget: number }>;
  tables: { on: string; map: Record<string, Record<string, string>> }[];
  lets: { name: string; when?: unknown; value: string }[];
  defs: Record<string, Node>;
  root: Node;
}
const lib = partsJson as unknown as Library;
const HEX = /^#[0-9A-Fa-f]{6}$/;

type Vars = Record<string, string>;

function flatten(spec: CharacterSpecV2): Vars {
  const v: Vars = {};
  const walk = (prefix: string, value: unknown) => {
    if (typeof value === "string") v[prefix] = value;
    else if (value && typeof value === "object" && !Array.isArray(value))
      for (const [k, child] of Object.entries(value))
        walk(prefix ? `${prefix}_${k}` : k, child);
  };
  walk("", spec.body);
  for (const [k, child] of Object.entries(spec.clothes))
    if (k !== "accessories" && k !== "pattern") walk(k, child);
  v.pattern = spec.clothes.pattern;
  for (const kind of [
    "scarf",
    "necklace",
    "badge",
    "backpack",
    "headphones",
    "bow_tie",
    "tie",
    "watch",
    "gloves",
    "hat",
  ])
    v[`acc_${kind}`] = "";
  for (const a of spec.clothes.accessories) v[`acc_${a.kind}`] = a.color;
  for (const f of ["freckles", "rosy_cheeks", "vitiligo_patches", "birthmark"])
    v[`feat_${f}`] = "";
  const body = spec.body as { skin_features?: string[] };
  for (const f of body.skin_features ?? []) v[`feat_${f}`] = "1";
  return v;
}

function shade(hex: string, dark: boolean): string {
  const f = (c: number) =>
    dark ? Math.floor((c * 78) / 100) : c + Math.floor(((255 - c) * 40) / 100);
  const h = (i: number) =>
    f(parseInt(hex.slice(1 + i * 2, 3 + i * 2), 16))
      .toString(16)
      .toUpperCase()
      .padStart(2, "0");
  return `#${h(0)}${h(1)}${h(2)}`;
}

function condition(cond: unknown, vars: Vars): boolean {
  if (!cond || typeof cond !== "object") return true;
  const m = cond as Record<string, unknown>;
  if (Array.isArray(m.all)) return m.all.every((c) => condition(c, vars));
  if (Array.isArray(m.any)) return m.any.some((c) => condition(c, vars));
  const value = vars[m.var as string] ?? "";
  if (Array.isArray(m.in)) return m.in.includes(value);
  if (Array.isArray(m.not_in)) return !m.not_in.includes(value);
  return true;
}

function evalToken(expr: string, vars: Vars): string {
  if (!/[+-]/.test(expr)) {
    const v = vars[expr];
    if (v === undefined) throw new Error(`rig: unknown variable "${expr}"`);
    return v;
  }
  let total = 0;
  let sign = 1;
  let start = 0;
  const flush = (end: number) => {
    const operand = expr.slice(start, end);
    let n = /^\d+$/.test(operand) ? parseInt(operand, 10) : NaN;
    if (Number.isNaN(n)) {
      const v = vars[operand];
      if (v === undefined)
        throw new Error(`rig: unknown variable "${operand}"`);
      n = parseInt(v, 10);
      if (!/^-?\d+$/.test(v))
        throw new Error(`rig: variable "${operand}" is not an integer`);
    }
    total += sign * n;
  };
  for (let i = 0; i < expr.length; i++) {
    if ((expr[i] === "+" || expr[i] === "-") && i > 0) {
      flush(i);
      sign = expr[i] === "-" ? -1 : 1;
      start = i + 1;
    }
  }
  flush(expr.length);
  return String(total);
}

function substitute(tpl: string, vars: Vars): string {
  if (!tpl.includes("{")) return tpl;
  let out = "";
  let rest = tpl;
  for (;;) {
    const i = rest.indexOf("{");
    if (i < 0) return out + rest;
    const j = rest.indexOf("}", i);
    if (j < 0) throw new Error("rig: unterminated token");
    out += rest.slice(0, i) + evalToken(rest.slice(i + 1, j), vars);
    rest = rest.slice(j + 1);
  }
}

function render(node: Node, vars: Vars, depth = 0): string {
  if (depth > 32) throw new Error("rig: nesting too deep");
  if (node === null || node === undefined) return "";
  if (typeof node === "string") return substitute(node, vars);
  if (Array.isArray(node))
    return node.map((n) => render(n, vars, depth + 1)).join("");
  const n = node as Record<string, unknown>;
  if (typeof n.ref === "string") {
    if (!(n.ref in lib.defs)) throw new Error(`rig: unknown def "${n.ref}"`);
    return render(lib.defs[n.ref], vars, depth + 1);
  }
  if (typeof n.by === "string") {
    const cases = (n.cases ?? {}) as Record<string, Node>;
    const key = vars[n.by] ?? "";
    return render(
      Object.prototype.hasOwnProperty.call(cases, key)
        ? cases[key]
        : (n.default as Node),
      vars,
      depth + 1,
    );
  }
  if ("when" in n)
    return render(
      condition(n.when, vars) ? (n.then as Node) : (n.else as Node),
      vars,
      depth + 1,
    );
  throw new Error("rig: unknown node");
}

function rigVars(
  spec: CharacterSpecV2,
  view: string,
  pose: string,
  expression: string,
): Vars {
  const vars = flatten(spec);
  vars.view = view;
  vars.pose = pose;
  vars.expression = expression;
  for (const t of lib.tables) {
    const value = vars[t.on];
    if (value !== undefined) Object.assign(vars, t.map[value] ?? {});
  }
  const set = new Set<string>();
  for (const l of lib.lets) {
    if (set.has(l.name) || !condition(l.when, vars)) continue;
    vars[l.name] = substitute(l.value, vars);
    set.add(l.name);
  }
  const names = Object.keys(vars).sort();
  for (const k of names)
    if (HEX.test(vars[k]!)) {
      vars[`${k}_dk`] = shade(vars[k]!, true);
      vars[`${k}_lt`] = shade(vars[k]!, false);
    }
  let h = 2166136261;
  const bytes = new TextEncoder();
  for (const k of names)
    for (const b of bytes.encode(`${k}=${vars[k]};`))
      h = Math.imul(h ^ b, 16777619) >>> 0;
  vars.uid = `c${h.toString(16).padStart(8, "0")}`;
  return vars;
}

/** Deterministic SVG for a validated v2 document; byte-identical to CompileV2 in Go. */
export function compileV2(
  spec: CharacterSpecV2,
  view: RigView,
  pose: Pose,
  expression: ExpressionV2,
): string {
  const v = lib.views[view];
  if (!v) throw new Error(`unsupported view: ${view}`);
  if (!POSES.includes(pose)) throw new Error(`unsupported pose: ${pose}`);
  if (!EXPRESSIONS_V2.includes(expression))
    throw new Error(`unsupported expression: ${expression}`);
  const vars = rigVars(spec, view, pose, expression);
  const svg =
    `<svg xmlns="http://www.w3.org/2000/svg" viewBox="${v.viewBox}" class="character-rig" data-view="${view}" data-pose="${pose}" data-expression="${expression}">` +
    render(lib.root, vars) +
    "</svg>";
  if (new TextEncoder().encode(svg).length > v.budget)
    throw new Error(`rig: ${view} render exceeds budget ${v.budget}`);
  return svg;
}

export const RIG_BUDGETS = {
  avatar: lib.views.avatar!.budget,
  body: lib.views.body!.budget,
};
