// Release body: changelog, download links and the QR code image.
import { readFileSync, writeFileSync } from "node:fs";
import { pathToFileURL } from "node:url";

export const APK_NAME = "duolify.apk";
export const QR_NAME = "duolify-android-qr.png";
export const IOS_NAME = "duolify-ios-simulator.zip";

export function releaseNotes({ repo, tag, changelog }) {
  const base = `https://github.com/${repo}/releases/download/${tag}`;
  return [
    changelog.trim(),
    "",
    "## Install",
    "",
    `- Android: scan the code or download [${APK_NAME}](${base}/${APK_NAME}), allow installs from this source and open the file.`,
    `- iOS (simulator only): download [${IOS_NAME}](${base}/${IOS_NAME}), unzip it and run \`xcrun simctl install booted Duolify.app\`.`,
    "",
    `![Android APK QR code](${base}/${QR_NAME})`,
    "",
    "Verify downloads with `SHA256SUMS`. See `docs/mobile-release.md` for details.",
    "",
  ].join("\n");
}

if (import.meta.url === pathToFileURL(process.argv[1] ?? "").href) {
  const [repo, tag, changelogFile, out] = process.argv.slice(2);
  if (!out) throw new Error("usage: release-notes.mjs <owner/repo> <tag> <changelog.md> <out.md>");
  writeFileSync(out, releaseNotes({ repo, tag, changelog: readFileSync(changelogFile, "utf8") }));
}
