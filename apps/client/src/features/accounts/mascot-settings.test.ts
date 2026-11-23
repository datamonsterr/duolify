import { describe, expect, it } from "vitest";
import { paletteFor } from "../../components/mascot";
import {
  changedFields,
  fromServerMascot,
  toServerMascotPatch,
} from "./mascot-settings";

describe("mascot settings mapping", () => {
  it("reads the server object and falls back to defaults for anything invalid", () => {
    const p = fromServerMascot({
      mode: "default",
      palette: paletteFor("blue"),
      accessories: ["bowtie"],
      companion_visible: false,
    });
    expect(p).toMatchObject({
      swatch: "blue",
      accessories: ["bowtie"],
      companionVisible: false,
      mode: "default",
    });
    expect(
      fromServerMascot({
        palette: { body: "#123456" },
        accessories: ["crown", "a", "b"],
      }).accessories,
    ).toEqual([]);
    expect(fromServerMascot(null).mode).toBe("default");
    expect(fromServerMascot({ mode: "character" }).mode).toBe("default");
  });
  it("sends only the touched fields and reports content-free field names", () => {
    expect(toServerMascotPatch({ companionVisible: false })).toEqual({
      companion_visible: false,
    });
    expect(toServerMascotPatch({ swatch: "rose" })).toEqual({
      palette: paletteFor("rose"),
    });
    expect(toServerMascotPatch({ swatch: undefined })).toEqual({
      palette: null,
    });
    expect(changedFields({ swatch: "rose", accessories: [] })).toEqual([
      "palette",
      "accessories",
    ]);
    expect(changedFields({ mode: "character", characterId: "x" })).toEqual([
      "character",
    ]);
    expect(changedFields({ companionVisible: true })).toEqual([]);
  });
});
