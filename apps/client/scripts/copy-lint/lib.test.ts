// @vitest-environment node
import { describe, expect, it } from "vitest";
import { readFileSync, writeFileSync, mkdtempSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import {
  checkString,
  countChars,
  countWords,
  fkGrade,
  isExempt,
  loadConfig,
  roleFromKey,
  scanLiterals,
} from "./lib.ts";

const cfg = loadConfig(join(import.meta.dirname, "../.."));

describe("counters", () => {
  it("counts words and characters, ignoring placeholders and punctuation", () => {
    expect(countWords("Save note")).toBe(2);
    expect(countWords("Ask · {cost} credits")).toBe(3);
    expect(countWords("")).toBe(0);
    expect(countChars("مرحبا")).toBe(5);
  });
  it("derives the role from the key suffix", () => {
    expect(roleFromKey("review.confirm_title", cfg)).toBe("heading");
    expect(roleFromKey("outline.goal_error", cfg)).toBe("error");
    expect(roleFromKey("settings.motion.hint", cfg)).toBe("caption");
    expect(roleFromKey("review.post", cfg)).toBeUndefined();
  });
  it("scores Flesch-Kincaid grade higher for harder text", () => {
    const easy = fkGrade("Tap the card to start your lesson.");
    const hard = fkGrade(
      "Authentication credentials necessitate comprehensive reconfiguration beforehand.",
    );
    expect(easy).toBeLessThan(5);
    expect(hard).toBeGreaterThan(14);
  });
});

describe("checkString", () => {
  it("applies role limits, with 1.3x characters for Arabic", () => {
    expect(
      checkString("a.title", "One two three four five", "en", cfg),
    ).toContain("heading-words");
    const long = "x".repeat(30);
    expect(checkString("a.title", long, "en", cfg)).toContain("heading-chars");
    expect(checkString("a.title", "x".repeat(30), "ar", cfg)).not.toContain(
      "heading-chars",
    );
    expect(checkString("a.title", "x".repeat(32), "ar", cfg)).toContain(
      "heading-chars",
    );
  });
  it("flags the denylist and suggests nothing for Arabic", () => {
    expect(checkString("a.x", "Utilize this", "en", cfg)).toContain(
      "denylist:utilize",
    );
    expect(checkString("a.x", "Utilize this", "ar", cfg)).toEqual([]);
  });
  it("flags grade above 7 only for five words or more", () => {
    expect(checkString("a.x", "Documentation", "en", cfg)).toEqual([]);
    expect(
      checkString(
        "a.x",
        "Comprehensive organizational infrastructure necessitates documentation requirements",
        "en",
        cfg,
      ),
    ).toContain("fk-grade");
  });
  it("exempts disclosures by id", () => {
    expect(isExempt("voice.explain_body", cfg)).toBe(true);
    expect(checkString("privacy.note", "x".repeat(200), "en", cfg)).toEqual([]);
  });
});

describe("literal scan", () => {
  const scan = (src: string) => {
    const dir = mkdtempSync(join(tmpdir(), "copy-"));
    const file = join(dir, "A.tsx");
    writeFileSync(file, src);
    return scanLiterals(file, cfg, dir).map((l) => l.text);
  };
  it("finds JSX text, visible attributes and label properties", () => {
    expect(
      scan(`const x = <button aria-label="Close" title={on ? "Mute" : "Unmute"} className="btn">Retry now</button>;
const tabs = [{ label: "Learn", id: "learn" }];`),
    ).toEqual(
      expect.arrayContaining(["Retry now", "Close", "Mute", "Unmute", "Learn"]),
    );
  });
  it("ignores class names, symbols and data-user-content", () => {
    expect(
      scan(
        `const x = <p data-user-content>Course text here</p>; const y = <i className="icon">·</i>;`,
      ),
    ).toEqual([]);
  });
  it("ignores the real catalogue files", () => {
    expect(
      readFileSync(
        join(import.meta.dirname, "../../copy-lint.config.json"),
        "utf8",
      ),
    ).toContain("sourceIgnore");
  });
});
