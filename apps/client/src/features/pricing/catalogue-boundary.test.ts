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
  it("keeps price and credit literals out of application code", () => {
    for (const [path, source] of Object.entries(sources)) {
      expect(source, path).not.toMatch(/\$\d|\b\d[\d,.]*\s+credits\b/i);
      expect(source, path).not.toMatch(
        /\b(?:credits|amount_minor|monthly_credits|concurrent_generation)\s*:\s*\d/,
      );
      expect(source, path).not.toMatch(/\.credits\s*(?:\?\?|\|\||=)\s*\d/);
      expect(source, path).not.toMatch(
        /import\s+.*from\s+["'][^"']*fixtures\//,
      );
    }
  });
});
