import assert from "node:assert/strict";
import test from "node:test";
import { renderChangelog, parseSubject } from "./changelog.mjs";
import { verifyCertOutput } from "./verify-apk-cert.mjs";
import { releaseNotes } from "./release-notes.mjs";
import { checkVersions } from "./check-version.mjs";
import {
  patchGradle,
  keystoreProperties,
} from "../../apps/client/scripts/prepare-android-signing.mjs";

const SHA =
  "37:83:6C:F7:ED:0F:11:40:20:66:B0:3D:E1:4C:77:14:F3:82:EC:A5:02:C6:ED:9F:0A:8C:40:73:05:82:DD:89";
const out = (n, sha) =>
  `Verifies\n${Array.from({ length: n }, (_, i) => `Signer #${i + 1} certificate SHA-256 digest: ${sha}`).join("\n")}\n`;

test("parses conventional subjects", () => {
  assert.deepEqual(parseSubject("feat(api)!: add x"), {
    type: "feat",
    scope: "api",
    breaking: true,
    text: "add x",
  });
  assert.equal(parseSubject("random words"), null);
});

test("changelog groups commits and skips merges", () => {
  const md = renderChangelog({
    version: "v0.2.0",
    previous: "v0.1.0",
    repo: "o/r",
    commits: [
      { sha: "aaaaaaaa1", subject: "feat(client): mobile builds" },
      { sha: "bbbbbbbb2", subject: "fix: crash" },
      { sha: "cccccccc3", subject: "Merge pull request #1" },
      { sha: "dddddddd4", subject: "free text" },
    ],
  });
  assert.match(md, /### Features\n- \*\*client:\*\* mobile builds \(aaaaaaa\)/);
  assert.match(md, /### Fixes\n- crash/);
  assert.match(md, /### Other changes\n- free text/);
  assert.doesNotMatch(md, /Merge pull request/);
  assert.match(md, /compare\/v0.1.0...v0.2.0/);
});

test("cert check accepts the expected single signer", () => {
  assert.equal(
    verifyCertOutput(out(1, SHA.replaceAll(":", "").toLowerCase()), SHA),
    SHA.replaceAll(":", "").toLowerCase(),
  );
});

test("cert check accepts apksigner verbose output", () => {
  const hex = SHA.replaceAll(":", "").toLowerCase();
  const verbose = `Verifies\nNumber of signers: 1\nV2 Signer: certificate DN: CN=x\nV2 Signer: certificate SHA-256 digest: ${hex}\n`;
  assert.equal(verifyCertOutput(verbose, SHA), hex);
  assert.throws(
    () => verifyCertOutput(verbose.replace("1\n", "2\n"), SHA),
    /exactly one signer/,
  );
  assert.throws(
    () => verifyCertOutput("Number of signers: 1\n", SHA),
    /no signer certificate/,
  );
});

test("cert check fails closed", () => {
  assert.throws(
    () => verifyCertOutput(out(1, "00".repeat(32)), SHA),
    /does not match/,
  );
  assert.throws(
    () => verifyCertOutput(out(2, SHA.replaceAll(":", "")), SHA),
    /exactly one signer/,
  );
  assert.throws(() => verifyCertOutput("", SHA), /exactly one signer/);
  assert.throws(
    () =>
      verifyCertOutput(
        "DOES NOT VERIFY\n" + out(1, SHA.replaceAll(":", "")),
        SHA,
      ),
    /does not verify/,
  );
  assert.throws(() => verifyCertOutput(out(1, SHA), "abc"), /SHA-256/);
});

test("release notes link the APK and embed the QR image", () => {
  const md = releaseNotes({
    repo: "o/r",
    tag: "v0.2.0",
    changelog: "## v0.2.0",
  });
  assert.match(md, /releases\/download\/v0.2.0\/duolify.apk/);
  assert.match(
    md,
    /!\[Android APK QR code\]\(https:\/\/github.com\/o\/r\/releases\/download\/v0.2.0\/duolify-android-qr.png\)/,
  );
});

test("version check matches tag", () => {
  assert.equal(checkVersions("v0.2.0", { a: "0.2.0" }), "0.2.0");
  assert.throws(() => checkVersions("v0.2.0", { a: "0.1.0" }), /tag wants/);
  assert.throws(() => checkVersions("0.2.0", {}), /not vMAJOR/);
});

const GRADLE = `android {\n    buildTypes {\n        getByName("debug") {\n        }\n        getByName("release") {\n            isMinifyEnabled = true\n        }\n    }\n}\n`;

test("signing patch touches release only and is idempotent", () => {
  const p = patchGradle(GRADLE);
  assert.match(
    p,
    /getByName\("release"\) \{\n\s+signingConfig = signingConfigs.getByName\("release"\)/,
  );
  assert.equal((p.match(/signingConfig = /g) ?? []).length, 1);
  assert.ok(p.indexOf("signingConfigs {") < p.indexOf("buildTypes {"));
  assert.match(
    p,
    /^import java.io.FileInputStream\nimport java.util.Properties\n/,
  );
  assert.equal(patchGradle(p), p);
  const withProps = patchGradle("import java.util.Properties\n" + GRADLE);
  assert.equal(
    (withProps.match(/import java.util.Properties/g) ?? []).length,
    1,
  );
  assert.match(withProps, /import java.io.FileInputStream/);
  assert.throws(() => patchGradle("android {}"), /no buildTypes/);
});

test("keystore properties require every credential", () => {
  assert.throws(
    () => keystoreProperties({}, "/k.jks"),
    /missing Android signing inputs/,
  );
  assert.throws(
    () =>
      keystoreProperties(
        {
          ANDROID_KEY_ALIAS: "a",
          ANDROID_KEYSTORE_PASSWORD: "b",
          ANDROID_KEY_PASSWORD: "c",
        },
        "",
      ),
    /missing/,
  );
  assert.match(
    keystoreProperties(
      {
        ANDROID_KEY_ALIAS: "a",
        ANDROID_KEYSTORE_PASSWORD: "b",
        ANDROID_KEY_PASSWORD: "c",
      },
      "/k.jks",
    ),
    /storeFile=\/k.jks/,
  );
});
