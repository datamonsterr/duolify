// Copy-budget lint library (openspec/changes/simplify-ux-gestures-hotkeys, tasks 1.x).
// Pure functions so the CLI, the audit and the tests share one implementation.
import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import { dirname, join, relative, resolve } from "node:path";
import ts from "typescript";

export type Role =
  "heading" | "button" | "caption" | "toast" | "coach" | "error" | "chat";
export type Limits = Partial<Record<Role, { words?: number; chars?: number }>>;
export type Config = {
  arLimitFactor: number;
  fk: { minWords: number; maxGrade: number };
  limits: Limits;
  roleSuffixes: Record<Role, string[]>;
  exemptKeyPattern: string;
  denylist: Record<string, string>;
  visibleAttributes: string[];
  visibleProperties: string[];
  catalogues: { file: string; en: string; ar: string }[];
  sourceIgnore: string[];
  screens: ScreenDef[];
};
export type ScreenDef = {
  id: string;
  kind?: "screen" | "sheet";
  prefixes?: string[];
  paths?: string[];
};

export function loadConfig(root: string): Config {
  return JSON.parse(readFileSync(join(root, "copy-lint.config.json"), "utf8"));
}

// ---- counters -------------------------------------------------------------

const placeholder = /\{[^}]*\}/g;
export function plain(text: string): string {
  return text.replace(placeholder, " ").replace(/\s+/g, " ").trim();
}
export function countWords(text: string): number {
  const t = text.replace(placeholder, "x").trim();
  return t ? t.split(/\s+/).filter((w) => /[\p{L}\p{N}]/u.test(w)).length : 0;
}
export function countChars(text: string): number {
  return [...text].length;
}
export function syllables(word: string): number {
  const w = word.toLowerCase().replace(/[^a-z]/g, "");
  if (!w) return 0;
  if (w.length <= 3) return 1;
  const stripped = w
    .replace(/(?:[^laeiouy]es|ed|[^laeiouy]e)$/, "")
    .replace(/^y/, "");
  const groups = stripped.match(/[aeiouy]{1,2}/g);
  return Math.max(1, groups ? groups.length : 1);
}
/** Flesch-Kincaid grade level, computed offline. */
export function fkGrade(text: string): number {
  const t = plain(text);
  const words = t.match(/[A-Za-z']+/g) ?? [];
  if (!words.length) return 0;
  const sentences = Math.max(1, (t.match(/[.!?]+(\s|$)/g) ?? []).length);
  const syl = words.reduce((n, w) => n + syllables(w), 0);
  return (
    0.39 * (words.length / sentences) + 11.8 * (syl / words.length) - 15.59
  );
}

export function roleFromKey(id: string, cfg: Pick<Config, "roleSuffixes">) {
  const last = id.split(".").pop() ?? id;
  const token = last.split(/[_-]/).pop() ?? last;
  for (const [role, suffixes] of Object.entries(cfg.roleSuffixes))
    if (suffixes.includes(token)) return role as Role;
  return undefined;
}
export function isExempt(id: string, cfg: Pick<Config, "exemptKeyPattern">) {
  return new RegExp(cfg.exemptKeyPattern, "i").test(id);
}

/** Rule names a string breaks, for one locale. Empty when within budget. */
export function checkString(
  id: string,
  text: string,
  locale: "en" | "ar",
  cfg: Config,
): string[] {
  if (isExempt(id, cfg)) return [];
  const out: string[] = [];
  const role = roleFromKey(id, cfg);
  const factor = locale === "ar" ? cfg.arLimitFactor : 1;
  const lim = role ? cfg.limits[role] : undefined;
  if (role && lim) {
    if (lim.words !== undefined && countWords(text) > lim.words)
      out.push(`${role}-words`);
    if (lim.chars !== undefined && countChars(text) > lim.chars * factor)
      out.push(`${role}-chars`);
  }
  if (locale === "en") {
    for (const w of text.toLowerCase().match(/[a-z]+/g) ?? [])
      if (cfg.denylist[w]) out.push(`denylist:${w}`);
    if (countWords(text) >= cfg.fk.minWords && fkGrade(text) > cfg.fk.maxGrade)
      out.push("fk-grade");
  }
  return [...new Set(out)];
}
export function suggestion(rule: string, cfg: Config): string {
  if (rule.startsWith("denylist:"))
    return `use "${cfg.denylist[rule.slice(9)]}"`;
  if (rule === "fk-grade")
    return `shorten to Flesch-Kincaid grade ${cfg.fk.maxGrade} or lower`;
  const [role, what] = rule.split("-");
  const l = cfg.limits[role as Role];
  return `${role} allows at most ${what === "words" ? l?.words + " words" : l?.chars + " characters"}`;
}

// ---- catalogue parsing -----------------------------------------------------

export type Catalogue = Map<string, { texts: string[]; file: string }>;

function parse(file: string) {
  return ts.createSourceFile(
    file,
    readFileSync(file, "utf8"),
    ts.ScriptTarget.Latest,
    true,
    file.endsWith("x") ? ts.ScriptKind.TSX : ts.ScriptKind.TS,
  );
}
function unwrap(e: ts.Expression): ts.Expression {
  while (
    ts.isSatisfiesExpression(e) ||
    ts.isAsExpression(e) ||
    ts.isParenthesizedExpression(e)
  )
    e = e.expression;
  return e;
}
function stringValue(e: ts.Expression): string | undefined {
  e = unwrap(e);
  if (ts.isStringLiteral(e) || ts.isNoSubstitutionTemplateLiteral(e))
    return e.text;
  if (
    ts.isBinaryExpression(e) &&
    e.operatorToken.kind === ts.SyntaxKind.PlusToken
  ) {
    const a = stringValue(e.left);
    const b = stringValue(e.right);
    return a !== undefined && b !== undefined ? a + b : undefined;
  }
  return undefined;
}
function resolveModule(from: string, spec: string): string | undefined {
  const base = resolve(dirname(from), spec);
  for (const c of [base, `${base}.ts`, `${base}.tsx`, join(base, "index.ts")])
    if (existsSync(c) && statSync(c).isFile()) return c;
  return undefined;
}
function findObject(
  file: string,
  name: string,
): { file: string; obj: ts.ObjectLiteralExpression } | undefined {
  const sf = parse(file);
  for (const st of sf.statements) {
    if (ts.isVariableStatement(st))
      for (const d of st.declarationList.declarations)
        if (ts.isIdentifier(d.name) && d.name.text === name && d.initializer) {
          const init = unwrap(d.initializer);
          if (ts.isObjectLiteralExpression(init)) return { file, obj: init };
        }
    if (
      ts.isImportDeclaration(st) &&
      st.importClause?.namedBindings &&
      ts.isNamedImports(st.importClause.namedBindings)
    )
      for (const el of st.importClause.namedBindings.elements)
        if (el.name.text === name && ts.isStringLiteral(st.moduleSpecifier)) {
          const target = resolveModule(file, st.moduleSpecifier.text);
          if (target)
            return findObject(target, (el.propertyName ?? el.name).text);
        }
  }
  return undefined;
}
function collect(
  file: string,
  obj: ts.ObjectLiteralExpression,
  into: Catalogue,
) {
  for (const p of obj.properties) {
    if (ts.isSpreadAssignment(p) && ts.isIdentifier(p.expression)) {
      const target = findObject(file, p.expression.text);
      if (target) collect(target.file, target.obj, into);
      continue;
    }
    if (!ts.isPropertyAssignment(p)) continue;
    const key =
      ts.isIdentifier(p.name) || ts.isStringLiteral(p.name)
        ? p.name.text
        : undefined;
    if (key === undefined) continue;
    const init = unwrap(p.initializer);
    const single = stringValue(init);
    if (single !== undefined) into.set(key, { texts: [single], file });
    else if (ts.isObjectLiteralExpression(init)) {
      const texts: string[] = [];
      for (const q of init.properties)
        if (ts.isPropertyAssignment(q)) {
          const v = stringValue(q.initializer);
          if (v !== undefined) texts.push(v);
        }
      if (texts.length) into.set(key, { texts, file });
    }
  }
}
export function readCatalogue(file: string, exportName: string): Catalogue {
  const found = findObject(file, exportName);
  const into: Catalogue = new Map();
  if (found) collect(found.file, found.obj, into);
  return into;
}

export function missingKeys(en: Catalogue, ar: Catalogue) {
  return {
    missingAr: [...en.keys()].filter((k) => !ar.has(k)),
    extraAr: [...ar.keys()].filter((k) => !en.has(k)),
  };
}

export type Violations = Record<string, string[]>; // id -> rules
export function catalogueViolations(
  cat: Catalogue,
  locale: "en" | "ar",
  cfg: Config,
): Violations {
  const out: Violations = {};
  for (const [id, { texts }] of cat) {
    const rules = new Set<string>();
    for (const t of texts)
      for (const r of checkString(id, t, locale, cfg)) rules.add(r);
    if (rules.size) out[id] = [...rules].sort();
  }
  return out;
}

// ---- literal scan ----------------------------------------------------------

export type Literal = { file: string; line: number; text: string };
const hasLetters = (s: string) => /\p{L}/u.test(s);

function literalsIn(
  e: ts.Expression,
  out: ts.Expression[] = [],
): ts.Expression[] {
  e = unwrap(e);
  if (ts.isConditionalExpression(e)) {
    literalsIn(e.whenTrue, out);
    literalsIn(e.whenFalse, out);
  } else if (
    ts.isBinaryExpression(e) &&
    [
      ts.SyntaxKind.AmpersandAmpersandToken,
      ts.SyntaxKind.BarBarToken,
      ts.SyntaxKind.QuestionQuestionToken,
    ].includes(e.operatorToken.kind)
  ) {
    literalsIn(e.right, out);
    if (e.operatorToken.kind !== ts.SyntaxKind.AmpersandAmpersandToken)
      literalsIn(e.left, out);
  } else if (ts.isTemplateExpression(e)) out.push(e);
  else if (stringValue(e) !== undefined) out.push(e);
  return out;
}
function textOf(e: ts.Expression): string {
  return ts.isTemplateExpression(e)
    ? e.head.text + e.templateSpans.map((s) => " " + s.literal.text).join("")
    : (stringValue(e) ?? "");
}
function hasUserContentAncestor(n: ts.Node): boolean {
  for (let p: ts.Node | undefined = n; p; p = p.parent) {
    const attrs = ts.isJsxElement(p)
      ? p.openingElement.attributes
      : ts.isJsxSelfClosingElement(p)
        ? p.attributes
        : undefined;
    if (
      attrs?.properties.some(
        (a) => ts.isJsxAttribute(a) && a.name.getText() === "data-user-content",
      )
    )
      return true;
  }
  return false;
}

export function scanLiterals(
  file: string,
  cfg: Config,
  rootForRel: string,
): Literal[] {
  const sf = parse(file);
  const rel = relative(rootForRel, file);
  const found: Literal[] = [];
  const add = (n: ts.Node, text: string) => {
    if (!hasLetters(text) || hasUserContentAncestor(n)) return;
    found.push({
      file: rel,
      line: sf.getLineAndCharacterOfPosition(n.getStart(sf)).line + 1,
      text: text.trim(),
    });
  };
  const visit = (n: ts.Node) => {
    if (ts.isJsxText(n)) {
      const t = n.text.replace(/\s+/g, " ").trim();
      if (t) add(n, t);
    } else if (
      ts.isJsxExpression(n) &&
      n.expression &&
      (ts.isJsxElement(n.parent) || ts.isJsxFragment(n.parent))
    ) {
      for (const l of literalsIn(n.expression)) add(l, textOf(l));
    } else if (
      ts.isJsxAttribute(n) &&
      cfg.visibleAttributes.includes(n.name.getText()) &&
      n.initializer
    ) {
      if (ts.isStringLiteral(n.initializer)) add(n, n.initializer.text);
      else if (ts.isJsxExpression(n.initializer) && n.initializer.expression)
        for (const l of literalsIn(n.initializer.expression)) add(l, textOf(l));
    } else if (
      ts.isPropertyAssignment(n) &&
      (ts.isIdentifier(n.name) || ts.isStringLiteral(n.name)) &&
      cfg.visibleProperties.includes(n.name.text)
    ) {
      const v = stringValue(n.initializer);
      if (v !== undefined && (/\s/.test(v.trim()) || /^[A-Z]/.test(v)))
        add(n, v);
    }
    ts.forEachChild(n, visit);
  };
  visit(sf);
  return found;
}

export function sourceFiles(root: string, cfg: Config): string[] {
  const out: string[] = [];
  const walk = (dir: string) => {
    for (const name of readdirSync(dir)) {
      const full = join(dir, name);
      const rel = relative(root, full).split("\\").join("/");
      if (statSync(full).isDirectory()) {
        if (
          !cfg.sourceIgnore.some(
            (p) => p.endsWith("/") && `${rel}/`.startsWith(p),
          )
        )
          walk(full);
      } else if (
        /\.(ts|tsx)$/.test(name) &&
        !/\.(test|spec)\.(ts|tsx)$/.test(name) &&
        !name.endsWith(".d.ts") &&
        !cfg.sourceIgnore.includes(rel)
      )
        out.push(full);
    }
  };
  walk(join(root, "src"));
  return out.sort();
}

// ---- baseline --------------------------------------------------------------

export type Baseline = {
  literals: Record<string, number>;
  catalogue: { en: Violations; ar: Violations };
};
export function emptyBaseline(): Baseline {
  return { literals: {}, catalogue: { en: {}, ar: {} } };
}
export function literalCounts(literals: Literal[]): Record<string, number> {
  const counts: Record<string, number> = {};
  for (const l of literals) counts[l.file] = (counts[l.file] ?? 0) + 1;
  return Object.fromEntries(
    Object.entries(counts).sort(([a], [b]) => a.localeCompare(b)),
  );
}

export type Finding = { message: string };
/** Compare the current state with the baseline; baselines may only go down. */
export function compareToBaseline(
  literals: Literal[],
  viol: { en: Violations; ar: Violations },
  parity: { missingAr: string[]; extraAr: string[] },
  base: Baseline,
  cfg: Config,
): { errors: string[]; stale: string[] } {
  const errors: string[] = [];
  const stale: string[] = [];
  for (const k of parity.missingAr)
    errors.push(
      `catalogue: key "${k}" is missing in the ar catalogue (add it next to the en key)`,
    );
  for (const k of parity.extraAr)
    errors.push(`catalogue: key "${k}" exists in ar but not in en`);
  const counts = literalCounts(literals);
  for (const [file, n] of Object.entries(counts)) {
    const allowed = base.literals[file] ?? 0;
    if (n > allowed) {
      const lines = literals.filter((l) => l.file === file);
      const shown = lines
        .slice(-(n - allowed))
        .map((l) => `  ${l.file}:${l.line} "${l.text.slice(0, 60)}"`);
      errors.push(
        `literal outside catalogue: ${file} has ${n} (baseline ${allowed}); add a catalogue id in messages.ts for each:\n${shown.join("\n")}`,
      );
    } else if (n < allowed) stale.push(`${file}: ${n} < baseline ${allowed}`);
  }
  for (const [file, allowed] of Object.entries(base.literals))
    if (!(file in counts) && allowed > 0)
      stale.push(`${file}: 0 < baseline ${allowed}`);
  for (const locale of ["en", "ar"] as const)
    for (const [id, rules] of Object.entries(viol[locale])) {
      const known = new Set(base.catalogue[locale][id] ?? []);
      for (const r of rules)
        if (!known.has(r))
          errors.push(
            `copy budget (${locale}): "${id}" breaks ${r} (role ${roleFromKey(id, cfg) ?? "none"}): ${suggestion(r, cfg)}`,
          );
    }
  for (const locale of ["en", "ar"] as const)
    for (const [id, rules] of Object.entries(base.catalogue[locale]))
      for (const r of rules)
        if (!(viol[locale][id] ?? []).includes(r))
          stale.push(`${locale} ${id}: ${r} now passes`);
  return { errors, stale };
}
