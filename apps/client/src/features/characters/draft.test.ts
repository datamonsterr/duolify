import { describe, expect, it } from "vitest";
import { speciesAttr, speciesOf } from "./catalog";
import {
  applyDraftPatch,
  changeKind,
  changeSpecies,
  defaultDraft,
} from "./draft";
import { validateSpecV2 } from "./schema-v2";

describe("draft", () => {
  it.each(["human", "animal", "robot", "fantasy"] as const)(
    "default %s is valid",
    (kind) => {
      expect(validateSpecV2(defaultDraft(kind)).valid).toBe(true);
    },
  );
  it("every species default and switch stays valid and species-allowed", () => {
    for (const kind of ["animal", "fantasy"] as const) {
      for (const sp of speciesOf(kind)) {
        const next = changeSpecies(defaultDraft(kind), sp);
        expect(validateSpecV2(next).valid, `${kind}/${sp}`).toBe(true);
        const attr = kind === "animal" ? "ears" : "wings";
        expect((next.body as unknown as Record<string, unknown>)[attr]).toBe(
          speciesAttr(kind, sp, attr)[0],
        );
      }
    }
  });
  it("kind switch keeps identity and resets the body", () => {
    const a = applyDraftPatch(defaultDraft("human"), {
      info: { name: "Mira" },
    });
    expect(a.ok).toBe(true);
    if (!a.ok) return;
    const b = changeKind(a.spec, "robot");
    expect(b.info.name).toBe("Mira");
    expect(b.body.kind).toBe("robot");
    expect(validateSpecV2(b).valid).toBe(true);
  });
  it("rejects a patch that breaks the schema", () => {
    expect(
      applyDraftPatch(defaultDraft("animal"), { body: { species: "dragon" } })
        .ok,
    ).toBe(false);
  });
});
