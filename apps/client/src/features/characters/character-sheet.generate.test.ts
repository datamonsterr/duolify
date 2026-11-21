// @vitest-environment node
// Generator, not a test: `mise run client:character-sheet` sets CHARACTER_SHEET_DIR.
import { mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { describe, it } from "vitest";
import { chromium } from "@playwright/test";
import { compileRigSvg, getDefaultCastFixtures } from "./rig";
import type { Expression } from "./types";

const outDir = process.env.CHARACTER_SHEET_DIR;
const expressions: Expression[] = [
  "idle",
  "happy",
  "sad",
  "thinking",
  "celebrate",
];
const slug = (name: string) =>
  name
    .replace(/^Provisional /, "")
    .toLowerCase()
    .replace(/\W+/g, "-");

const CELL = 160;
const GAP = 24;
const LABEL_W = 170;
const HEAD_H = 56;

function sheet(dark: boolean): string {
  const specs = getDefaultCastFixtures();
  const bg = dark ? "#16161d" : "#faf8f3";
  const fg = dark ? "#f2f2f2" : "#1a1a24";
  const width = LABEL_W + expressions.length * (CELL + GAP) + GAP;
  const height = HEAD_H + specs.length * (CELL + GAP) + GAP;
  const text = (
    x: number,
    y: number,
    s: string,
    size: number,
    anchor = "middle",
  ) =>
    `<text x="${x}" y="${y}" fill="${fg}" font-family="sans-serif" font-size="${size}" text-anchor="${anchor}">${s}</text>`;
  let body = `<rect width="${width}" height="${height}" fill="${bg}"/>`;
  expressions.forEach((e, c) => {
    body += text(LABEL_W + c * (CELL + GAP) + CELL / 2, 34, e, 18);
  });
  specs.forEach((spec, r) => {
    const y = HEAD_H + r * (CELL + GAP);
    body += text(16, y + CELL / 2 + 6, spec.info.name, 18, "start");
    expressions.forEach((e, c) => {
      const x = LABEL_W + c * (CELL + GAP);
      body += `<svg x="${x}" y="${y}" width="${CELL}" height="${CELL}" viewBox="0 0 160 160">${compileRigSvg(spec, e).replace(/^<svg[^>]*>|<\/svg>$/g, "")}</svg>`;
    });
  });
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${width} ${height}" width="${width}" height="${height}">${body}</svg>\n`;
}

describe.skipIf(!outDir)("default cast character sheet", () => {
  it("renders 3 characters x 5 expressions with the real rig", async () => {
    const dir = outDir as string;
    mkdirSync(dir, { recursive: true });
    for (const spec of getDefaultCastFixtures()) {
      for (const e of expressions) {
        writeFileSync(
          join(dir, `${slug(spec.info.name)}-${e}.svg`),
          `${compileRigSvg(spec, e)}\n`,
        );
      }
    }
    writeFileSync(join(dir, "sheet.svg"), sheet(false));
    writeFileSync(join(dir, "sheet-dark.svg"), sheet(true));

    const browser = await chromium.launch();
    try {
      const page = await browser.newPage({ deviceScaleFactor: 1 });
      await page.setContent(`<body style="margin:0">${sheet(false)}</body>`);
      await (await page.$("svg"))!.screenshot({
        path: join(dir, "sheet.png"),
      });
    } finally {
      await browser.close();
    }
  }, 60_000);
});
