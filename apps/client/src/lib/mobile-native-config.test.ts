// @vitest-environment node
import { execFileSync } from "node:child_process";
import { mkdirSync, mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { describe, expect, it } from "vitest";

const root = resolve(__dirname, "../..");
const read = (file: string) => readFileSync(join(root, file), "utf8");

describe("mobile native configuration", () => {
  it("declares the iOS microphone usage description", () => {
    const plist = read("src-tauri/Info.ios.plist");
    expect(plist).toMatch(
      /<key>NSMicrophoneUsageDescription<\/key>\s*<string>[^<]+<\/string>/,
    );
  });

  it("adds Android microphone permissions to the generated manifest", () => {
    const dir = mkdtempSync(join(tmpdir(), "manifest-"));
    const manifestDir = join(dir, "src-tauri/gen/android/app/src/main");
    mkdirSync(manifestDir, { recursive: true });
    const manifest = join(manifestDir, "AndroidManifest.xml");
    writeFileSync(
      manifest,
      '<manifest xmlns:android="http://schemas.android.com/apk/res/android">\n    <application></application>\n</manifest>\n',
    );
    const script = join(root, "scripts/prepare-android-manifest.mjs");
    for (let i = 0; i < 2; i++) execFileSync("node", [script], { cwd: dir });
    const result = readFileSync(manifest, "utf8");
    for (const permission of ["RECORD_AUDIO", "MODIFY_AUDIO_SETTINGS"])
      expect(result.split(`android.permission.${permission}`)).toHaveLength(2);
  });

  it.each(["tauri.android.conf.json", "tauri.ios.conf.json"])(
    "%s allows the staging origins",
    (file) => {
      const csp: string = JSON.parse(read(`src-tauri/${file}`)).app.security
        .csp;
      expect(csp).toContain("https://staging.duolify.online");
      expect(csp).toContain("https://staging-api.duolify.online");
    },
  );
});
