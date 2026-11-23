import { describe, expect, it } from "vitest";
import {
  MASCOT_SWATCHES,
  MASCOT_SWATCH_COLORS,
  paletteFor,
  swatchOf,
  validateRigPartSpec,
} from "./rig-part-spec";
import spec from "./rig-part-spec.v1.json";

const lum = (h: string) => {
  const c = [1, 3, 5]
    .map((i) => parseInt(h.slice(i, i + 2), 16) / 255)
    .map((v) => (v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4));
  return 0.2126 * c[0] + 0.7152 * c[1] + 0.0722 * c[2];
};
const ratio = (a: string, b: string) => {
  const [x, y] = [lum(a), lum(b)].sort((p, q) => q - p);
  return (x + 0.05) / (y + 0.05);
};

describe("RigPartSpec v1", () => {
  it("every swatch body passes 3:1 on both canvases", () => {
    for (const id of MASCOT_SWATCHES)
      for (const canvas of spec.canvases)
        expect(
          ratio(MASCOT_SWATCH_COLORS[id].body, canvas),
          `${id} on ${canvas}`,
        ).toBeGreaterThanOrEqual(spec.minContrast);
  });
  it("accepts allowlisted palettes and at most two accessories", () => {
    expect(
      validateRigPartSpec({
        rig: "robot-v1",
        palette: paletteFor("blue"),
        accessories: ["bowtie", "badge"],
      }),
    ).toBeTruthy();
    expect(swatchOf(paletteFor("pink"))).toBe("pink");
    expect(
      validateRigPartSpec({
        rig: "robot-v1",
        accessories: ["bowtie", "badge", "scarf"],
      }),
    ).toBeUndefined();
    expect(
      validateRigPartSpec({
        rig: "robot-v1",
        accessories: ["bowtie", "bowtie"],
      }),
    ).toBeUndefined();
    expect(
      validateRigPartSpec({ rig: "robot-v1", accessories: ["crown"] }),
    ).toBeUndefined();
    expect(
      validateRigPartSpec({ rig: "owl", accessories: [] }),
    ).toBeUndefined();
    expect(
      validateRigPartSpec({
        rig: "robot-v1",
        palette: { ...paletteFor("blue"), body: "#123456" },
        accessories: [],
      }),
    ).toBeUndefined();
  });
});
