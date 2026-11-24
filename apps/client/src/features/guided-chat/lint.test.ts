import { describe, expect, it } from "vitest";
import { en } from "../design/localization/messages";
import { ar } from "../design/localization/messages.ar";
import { lintScript } from "./lint";
import { registerFixtureCopy } from "./messages.fixture";
import { allScripts, fixtureTour } from "./scripts";
import type { GuidedScript } from "./types";

registerFixtureCopy();
const cat = { en, ar };
const clone = (): GuidedScript => structuredClone(fixtureTour);

describe("script lint", () => {
  it.each(allScripts.map((s) => [s.id, s] as const))(
    "%s is valid",
    (_id, script) => {
      expect(lintScript(script, cat)).toEqual([]);
    },
  );
  it("names a key missing from Arabic", () => {
    const { "fixture.name.q": _gone, ...rest } = ar as Record<string, unknown>;
    void _gone;
    expect(lintScript(fixtureTour, { en, ar: rest })).toContain(
      "missing ar key fixture.name.q",
    );
  });
  it("rejects an unknown branch target", () => {
    const s = clone();
    s.steps[3].next = [{ when: { eq: ["remind", false] }, to: "nowhere" }];
    expect(lintScript(s, cat).join()).toContain("unknown step nowhere");
  });
  it("rejects backward jumps", () => {
    const s = clone();
    s.steps[3].next = [{ when: { eq: ["remind", false] }, to: "name" }];
    expect(lintScript(s, cat).join()).toContain("jumps back");
  });
  it("rejects a validator looser than the API limit", () => {
    const s = clone();
    s.steps.find((x) => x.id === "notes")!.validate = {
      type: "string",
      max: 5000,
    };
    expect(lintScript(s, cat).join()).toContain("looser than the API limit");
  });
  it("rejects persistent secure steps", () => {
    const s = structuredClone(allScripts[1]);
    s.steps[2].persist = true;
    expect(lintScript(s, cat).join()).toContain("must not persist");
  });
  it("rejects code evaluation in expressions", () => {
    const s = clone();
    (s.steps[4] as { when: unknown }).when = { js: "1" };
    expect(lintScript(s, cat).join()).toContain("schema");
  });
});
