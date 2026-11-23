// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  cpSync,
  readFileSync,
  mkdirSync,
  mkdtempSync,
  writeFileSync,
} from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { analyse } from "./run.ts";
import { compareToBaseline, emptyBaseline, type Baseline } from "./lib.ts";

// Builds a tiny client tree: one catalogue pair (with a spread, like onboarding) and one component.
function fixture(opts: { ar?: string; en?: string; tsx?: string }) {
  const root = mkdtempSync(join(tmpdir(), "copy-fx-"));
  mkdirSync(join(root, "src/features/x"), { recursive: true });
  cpSync(
    join(import.meta.dirname, "../../copy-lint.config.json"),
    join(root, "copy-lint.config.json"),
  );
  writeFileSync(
    join(root, "src/features/x/messages.ts"),
    `const shared = { "x.save": "Save note" };
export const en = { ...shared, "x.title": "Notes", ${opts.en ?? ""} };
export const ar = { ...shared, "x.title": "ملاحظات", ${opts.ar ?? ""} };`,
  );
  writeFileSync(
    join(root, "src/features/x/View.tsx"),
    opts.tsx ?? "export const V = () => <i />;",
  );
  const cfgPath = join(root, "copy-lint.config.json");
  const cfg = JSON.parse(readFileSync(cfgPath, "utf8"));
  cfg.catalogues = [{ file: "src/features/x/messages.ts", en: "en", ar: "ar" }];
  cfg.sourceIgnore = ["src/features/x/messages.ts"];
  writeFileSync(cfgPath, JSON.stringify(cfg));
  return root;
}

const run = (root: string, base: Baseline = emptyBaseline()) => {
  const a = analyse(root);
  return compareToBaseline(a.literals, a.violations, a.parity, base, a.cfg);
};

describe("copy lint", () => {
  it("passes a clean tree and resolves spread catalogues", () => {
    const root = fixture({});
    expect(analyse(root).en.size).toBe(2);
    expect(run(root).errors).toEqual([]);
  });
  it("fails a literal outside the catalogue with file and line", () => {
    const root = fixture({
      tsx: "export const V = () => <button>Retry now</button>;",
    });
    const { errors } = run(root);
    expect(errors[0]).toMatch(/View\.tsx:1 "Retry now"/);
    expect(errors[0]).toMatch(/add a catalogue id/);
  });
  it("fails a key missing in ar", () => {
    const root = fixture({ en: `"x.extra": "More"` });
    expect(run(root).errors.join()).toMatch(
      /"x\.extra" is missing in the ar catalogue/,
    );
  });
  it("fails an over-budget string by role and names the limit", () => {
    const root = fixture({
      en: `"x.save_hint": "${"long ".repeat(20)}"`,
      ar: `"x.save_hint": "قصير"`,
    });
    expect(run(root).errors.join()).toMatch(
      /"x\.save_hint" breaks caption-chars \(role caption\): caption allows at most 60 characters/,
    );
  });
  it("fails a denylist word and a hard sentence", () => {
    const root = fixture({
      en: `"x.a": "Utilize it", "x.b": "Comprehensive organizational infrastructure necessitates documentation requirements"`,
      ar: `"x.a": "استخدمه", "x.b": "نص"`,
    });
    const errs = run(root).errors.join("\n");
    expect(errs).toMatch(/"x\.a" breaks denylist:utilize.*use "use"/);
    expect(errs).toMatch(/"x\.b" breaks fk-grade/);
  });
  it("lets a baselined literal pass and fails a raised count", () => {
    const root = fixture({
      tsx: "export const V = () => <button>Retry now</button>;",
    });
    const base = emptyBaseline();
    base.literals["src/features/x/View.tsx"] = 1;
    expect(run(root, base).errors).toEqual([]);
    const raised = fixture({
      tsx: "export const V = () => <p>One more</p>;export const W = () => <p>Two more</p>;",
    });
    expect(run(raised, base).errors.length).toBe(1);
  });
  it("reports lowered counts as stale, not as errors", () => {
    const root = fixture({});
    const base = emptyBaseline();
    base.literals["src/features/x/View.tsx"] = 3;
    const r = run(root, base);
    expect(r.errors).toEqual([]);
    expect(r.stale.length).toBe(1);
  });
});
