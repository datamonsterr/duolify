import { describe, expect, it } from "vitest";
import { hasCommercialPricingLiteral } from "./literal-rule";
import literalFixtures from "./fixtures/literal-rule.json";

const sources = import.meta.glob<string>(
  [
    "../../**/*.ts",
    "../../**/*.tsx",
    "!../../**/*.test.*",
    "!../../**/fixtures/**",
  ],
  { query: "?raw", import: "default", eager: true },
);
describe("pricing boundary", () => {
  it("classifies the authored commercial and documented noncommercial literal fixtures", () => {
    expect(literalFixtures.commercial.every(hasCommercialPricingLiteral)).toBe(
      true,
    );
    expect(
      literalFixtures.noncommercial.every(
        (line) => !hasCommercialPricingLiteral(line),
      ),
    ).toBe(true);
  });

  it("rejects commercial amounts outside authored fixtures and documented exceptions", () => {
    for (const [path, source] of Object.entries(sources)) {
      for (const [lineNumber, line] of source.split("\n").entries()) {
        expect(
          hasCommercialPricingLiteral(line),
          `${path}:${lineNumber + 1}`,
        ).toBe(false);
      }
      expect(source, path).not.toMatch(
        /import\s+.*from\s+["'][^"']*fixtures\//,
      );
    }
  });
});
