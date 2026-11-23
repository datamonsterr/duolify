import { createHash } from "node:crypto";
import { describe, expect, it } from "vitest";
import fixtures from "../../../../../services/api/internal/characters/schema/v2.fixtures.json";
import golden from "../../../../../services/api/internal/characters/rig/golden.v2.json";
import { compileV2, POSES, RIG_BUDGETS } from "./rig-v2";
import { altText } from "./rig-v2-alt";
import { validateSpecV2 } from "./schema-v2";
import { EXPRESSIONS_V2, type CharacterSpecV2 } from "./types-v2";

const specs = new Map<string, CharacterSpecV2>();
for (const c of fixtures.cases as {
  name: string;
  valid: boolean;
  spec: unknown;
}[]) {
  if (!c.valid) continue;
  const r = validateSpecV2(c.spec);
  if (r.valid) specs.set(c.name, r.spec);
}

describe("rig v2 (parity with the Go compiler)", () => {
  it("renders byte-identical SVG for every golden case", () => {
    expect(golden.cases.length).toBeGreaterThan(200);
    for (const g of golden.cases) {
      const svg = compileV2(
        specs.get(g.name)!,
        g.view as "avatar" | "body",
        g.pose as (typeof POSES)[number],
        g.expression as (typeof EXPRESSIONS_V2)[number],
      );
      const label = `${g.name} ${g.view}/${g.pose}/${g.expression}`;
      expect([label, new TextEncoder().encode(svg).length]).toEqual([
        label,
        g.bytes,
      ]);
      expect([label, createHash("sha256").update(svg).digest("hex")]).toEqual([
        label,
        g.sha256,
      ]);
    }
  });

  it("produces the same English alt text as Go", () => {
    for (const g of golden.cases.filter((c) => "alt" in c))
      expect(altText(specs.get(g.name)!)).toBe((g as { alt: string }).alt);
  });

  it("stays within budget with no scripts, external refs or animation", () => {
    for (const spec of specs.values())
      for (const view of ["avatar", "body"] as const) {
        const svg = compileV2(spec, view, "celebrate", "surprised");
        expect(new TextEncoder().encode(svg).length).toBeLessThanOrEqual(
          RIG_BUDGETS[view],
        );
        expect(svg).not.toMatch(
          /<script|href|<image|<use|<filter|<animate|<set |xlink|style=/,
        );
        const doc = new DOMParser().parseFromString(svg, "image/svg+xml");
        expect(doc.querySelector("parsererror")).toBeNull();
      }
  });

  it("rejects unknown views, poses and expressions", () => {
    const spec = specs.get("valid human")!;
    expect(() => compileV2(spec, "poster" as "body", "idle", "idle")).toThrow();
    expect(() => compileV2(spec, "body", "dance" as "idle", "idle")).toThrow();
    expect(() => compileV2(spec, "body", "idle", "angry" as "idle")).toThrow();
  });

  it("localizes alt text to Arabic without markup", () => {
    const text = altText(specs.get("valid human")!, "ar");
    expect(text).toContain("بالغ");
    expect(text).not.toMatch(/[<>]/);
  });
});
