/* global URL, process, console */
// Bundle size report and budget check for the web build (dist/).
// "Initial" = what index.html loads before first paint: its module script, modulepreloads and
// stylesheets. Usage: node scripts/bundle-size.mjs [--check] [--json]
import { readFileSync, readdirSync } from "node:fs";
import { gzipSync } from "node:zlib";
import { join } from "node:path";

const dist = new URL("../dist/", import.meta.url).pathname;
const budget = JSON.parse(
  readFileSync(new URL("../bundle-budget.json", import.meta.url), "utf8"),
);
const size = (file) => {
  const buf = readFileSync(join(dist, file));
  return { file, raw: buf.length, gzip: gzipSync(buf).length };
};
const html = readFileSync(join(dist, "index.html"), "utf8");
const initial = [
  ...html.matchAll(/(?:src|href)="\/(assets\/[^"]+\.(?:js|css))"/g),
].map((m) => m[1]);
const all = readdirSync(join(dist, "assets"))
  .filter((f) => /\.(js|css)$/.test(f))
  .map((f) => `assets/${f}`);
const sum = (rows, key) => rows.reduce((n, r) => n + r[key], 0);
const group = (files, ext) => files.filter((f) => f.endsWith(ext)).map(size);
const report = {
  initialJs: group(initial, ".js"),
  initialCss: group(initial, ".css"),
  allJs: group(all, ".js"),
  allCss: group(all, ".css"),
};
const totals = {
  initialJsGzip: sum(report.initialJs, "gzip"),
  initialJsRaw: sum(report.initialJs, "raw"),
  initialCssGzip: sum(report.initialCss, "gzip"),
  totalJsGzip: sum(report.allJs, "gzip"),
  totalJsRaw: sum(report.allJs, "raw"),
  totalCssGzip: sum(report.allCss, "gzip"),
};
if (process.argv.includes("--json")) {
  console.log(JSON.stringify({ ...report, totals }, null, 2));
} else {
  const kb = (n) => (n / 1024).toFixed(1).padStart(8);
  for (const [name, rows] of Object.entries(report)) {
    console.log(`${name}:`);
    for (const r of rows.sort((a, b) => b.gzip - a.gzip))
      console.log(`  ${kb(r.raw)} kB raw ${kb(r.gzip)} kB gzip  ${r.file}`);
  }
  console.log(totals);
}
if (process.argv.includes("--check")) {
  const failures = Object.entries(budget)
    .filter(([k, limit]) => totals[k] > limit)
    .map(([k, limit]) => `${k}: ${totals[k]} B exceeds budget ${limit} B`);
  if (failures.length) {
    console.error(`Bundle budget exceeded:\n${failures.join("\n")}`);
    process.exit(1);
  }
  console.log("Bundle budget OK");
}
