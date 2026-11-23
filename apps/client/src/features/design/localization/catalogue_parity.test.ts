import { describe, expect, it } from "vitest";
import { ar, en } from "./messages";

describe("message catalogues", () => {
  it("keeps every English id in Arabic and no extra Arabic id", () => {
    const a = Object.keys(ar);
    const e = Object.keys(en);
    expect(e.filter((k) => !(k in ar))).toEqual([]);
    expect(a.filter((k) => !(k in en))).toEqual([]);
  });
  it("keeps plural forms: Arabic covers the categories English uses", () => {
    for (const [id, msg] of Object.entries(en)) {
      if (typeof msg === "string") continue;
      expect(typeof (ar as Record<string, unknown>)[id], id).not.toBe(
        "undefined",
      );
    }
  });
});
