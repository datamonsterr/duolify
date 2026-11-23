import { describe, expect, it } from "vitest";
import fixtures from "../../../../../services/api/internal/characters/schema/v2.fixtures.json";
import golden from "../../../../../services/api/internal/characters/schema/v1-to-v2.golden.json";
import { validateSpecV2 } from "./schema-v2";
import { defaultSpecV2, readSpec, upgradeV1 } from "./upgrade";
import type { CharacterSpec } from "./types";

type Case = {
  name: string;
  valid: boolean;
  reason?: string;
  path?: string;
  spec: unknown;
};

describe("schema v2 shared fixtures (same file as Go)", () => {
  for (const c of fixtures.cases as Case[]) {
    it(c.name, () => {
      const result = validateSpecV2(c.spec);
      if (c.valid) {
        expect(result.valid).toBe(true);
        return;
      }
      expect(result).toMatchObject({
        valid: false,
        code: c.reason,
        path: c.path,
      });
    });
  }
});

describe("v1 to v2 upgrade", () => {
  for (const pair of golden.pairs) {
    it(`golden: ${pair.name}`, () => {
      expect(upgradeV1(pair.v1 as unknown as CharacterSpec)).toEqual(pair.v2);
      expect(validateSpecV2(pair.v2).valid).toBe(true);
      const read = readSpec(pair.v1);
      expect(read.ok && read.reading.upgraded).toBe(true);
    });
  }

  it("upgrades every v1 shape, eye, mouth and accessory subset to a valid v2 document", () => {
    const subsets = [
      [],
      ["glasses"],
      ["scarf"],
      ["hat"],
      ["badge"],
      ["glasses", "scarf"],
      ["glasses", "hat"],
      ["glasses", "badge"],
      ["scarf", "hat"],
      ["scarf", "badge"],
      ["hat", "badge"],
    ];
    let n = 0;
    for (const shape of ["round", "bean", "star"])
      for (const eyes of ["round", "oval", "sleepy"])
        for (const mouth of ["smile", "line", "open"])
          for (const accessories of subsets) {
            const v1 = structuredClone(
              golden.pairs[0]!.v1,
            ) as unknown as CharacterSpec;
            v1.appearance = {
              ...v1.appearance,
              body_shape: shape,
              eyes,
              mouth,
              accessories,
            } as CharacterSpec["appearance"];
            expect(validateSpecV2(upgradeV1(v1)).valid).toBe(true);
            n++;
          }
    expect(n).toBe(297);
  });

  it("degrades unknown future versions and catalog ids to a read-only default", () => {
    const good = (fixtures.cases as Case[])[0]!.spec as Record<string, unknown>;
    for (const doc of [
      { schema_version: 3 },
      { schema_version: 2, catalog_version: 9 },
      { ...good, body: { ...(good.body as object), face_shape: "hexagon" } },
    ]) {
      const read = readSpec(doc);
      expect(read.ok && read.reading.readOnly).toBe(true);
      expect(read.ok && read.reading.spec).toEqual(defaultSpecV2());
    }
    expect(readSpec(good)).toMatchObject({
      ok: true,
      reading: { readOnly: false, upgraded: false, sourceVersion: 2 },
    });
    expect(readSpec({ schema_version: 2, catalog_version: 1 })).toMatchObject({
      ok: false,
    });
    expect(readSpec([])).toMatchObject({ ok: false });
  });

  it("default document is itself valid", () => {
    expect(validateSpecV2(defaultSpecV2()).valid).toBe(true);
  });
});
