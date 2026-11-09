import { describe, expect, it } from "vitest";

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
  it("rejects commercial amounts outside authored fixtures and documented exceptions", () => {
    for (const [path, source] of Object.entries(sources)) {
      const rules = [
        /\$\s?\d[\d,.]*/,
        /\b\d[\d,.]*(?:\.\d+)?\s*(?:credits?|USD|\/\s?(?:month|year))\b/i,
        /\b(?:price|price_minor|amount_minor|credits|monthly_credits|daily_free_credits|concurrent_generation)\s*:\s*\d/i,
        /\b(?:price|price_minor|amount_minor|credits|monthly_credits|daily_free_credits|concurrent_generation)\s*=\s*\d/i,
        /\.credits\s*(?:\?\?|\|\||=)\s*\d/,
      ];
      for (const [lineNumber, line] of source.split("\n").entries()) {
        if (line.includes("@noncommercial-number")) continue;
        for (const rule of rules)
          expect(line, `${path}:${lineNumber + 1}`).not.toMatch(rule);
      }
      expect(source, path).not.toMatch(
        /import\s+.*from\s+["'][^"']*fixtures\//,
      );
    }
  });
});
