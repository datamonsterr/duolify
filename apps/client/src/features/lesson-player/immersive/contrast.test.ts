import { describe, expect, it } from "vitest";
import tokens from "../../../../../../docs/design/tokens.json";
import { composite, contrast, GLASS_ALPHA, parseHex } from "./contrast";

// The overlay uses --text only (never --muted): muted fails 4.5:1 over a white Slide on the dark surface.
const slides = { dark: [0, 0, 0], white: [255, 255, 255] } as const;

describe("translucent overlay surface", () => {
  for (const mode of ["light", "dark"] as const) {
    const colors = tokens.color[mode] as Record<
      string,
      { $value: { hex: string } }
    >;
    for (const [name, behind] of Object.entries(slides)) {
      for (const fg of ["text"]) {
        it(`${fg} keeps 4.5:1 on ${mode} surface at ${GLASS_ALPHA} over a ${name} slide`, () => {
          const bg = composite(
            parseHex(colors.surface.$value.hex),
            GLASS_ALPHA,
            [...behind],
          );
          expect(
            contrast(parseHex(colors[fg].$value.hex), bg),
          ).toBeGreaterThanOrEqual(4.5);
        });
      }
    }
  }
  it("computes known ratios", () => {
    expect(contrast([0, 0, 0], [255, 255, 255])).toBeCloseTo(21, 0);
    expect(composite([255, 255, 255], 0.5, [0, 0, 0])).toEqual([128, 128, 128]);
  });
});
