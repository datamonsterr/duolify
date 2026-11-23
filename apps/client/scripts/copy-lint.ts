// Copy-budget lint: catalogue parity, role budgets, plain words, literals outside catalogues.
// Usage: node scripts/copy-lint.ts [--update-baseline | --audit]
import { readFileSync, writeFileSync } from "node:fs";
import { join, resolve } from "node:path";
import { renderAudit } from "./copy-lint/audit.ts";
import {
  compareToBaseline,
  emptyBaseline,
  literalCounts,
  type Baseline,
} from "./copy-lint/lib.ts";
import { analyse } from "./copy-lint/run.ts";

const root = resolve(import.meta.dirname, "..");
const baselinePath = join(root, "copy-baseline.json");
const auditPath = resolve(root, "../../docs/design/copy-audit.md");
const a = analyse(root);

const sorted = <T>(o: Record<string, T>) =>
  Object.fromEntries(Object.entries(o).sort(([x], [y]) => x.localeCompare(y)));
const current = (): Baseline => ({
  literals: literalCounts(a.literals),
  catalogue: { en: sorted(a.violations.en), ar: sorted(a.violations.ar) },
});

if (process.argv.includes("--audit")) {
  writeFileSync(auditPath, renderAudit(a, root));
  console.log(`copy-audit: wrote ${auditPath}`);
} else if (process.argv.includes("--update-baseline")) {
  // Baselines only go down: refuse to write anything that raises a count or adds a violation.
  let old: Baseline = emptyBaseline();
  try {
    old = JSON.parse(readFileSync(baselinePath, "utf8"));
  } catch {
    /* first run */
  }
  const { errors } = compareToBaseline(
    a.literals,
    a.violations,
    a.parity,
    old,
    a.cfg,
  );
  const initial = Object.keys(old.literals).length === 0;
  if (errors.length && !initial && !process.argv.includes("--force-raise")) {
    console.error(errors.join("\n"));
    console.error(
      "copy-baseline: refusing to raise the baseline (fix the copy instead)",
    );
    process.exit(1);
  }
  writeFileSync(baselinePath, JSON.stringify(current(), null, 2) + "\n");
  console.log("copy-baseline: written");
} else {
  let base: Baseline = emptyBaseline();
  try {
    base = JSON.parse(readFileSync(baselinePath, "utf8"));
  } catch {
    /* none yet */
  }
  const { errors, stale } = compareToBaseline(
    a.literals,
    a.violations,
    a.parity,
    base,
    a.cfg,
  );
  if (stale.length)
    console.log(
      `copy-lint: ${stale.length} baseline entries can be lowered (mise run client:copy-baseline)`,
    );
  if (errors.length) {
    console.error(errors.join("\n"));
    console.error(`copy-lint: ${errors.length} problem(s)`);
    process.exit(1);
  }
  console.log(
    `copy-lint: ok (${a.en.size} ids, ${a.literals.length} baselined literals)`,
  );
}
