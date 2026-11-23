// Fail-closed APK signing check: exactly one signer whose certificate SHA-256
// equals the expected release certificate.
import { execFileSync } from "node:child_process";
import { existsSync, readdirSync, readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

export const normalizeDigest = (value) =>
  value.replace(/[:\s]/g, "").toLowerCase();

export function verifyCertOutput(output, expected) {
  const want = normalizeDigest(expected);
  if (!/^[0-9a-f]{64}$/.test(want))
    throw new Error("expected certificate digest is not SHA-256");
  if (/DOES NOT VERIFY/i.test(output))
    throw new Error("APK signature does not verify");
  // Verbose output lists "Number of signers" and one digest per scheme
  // ("V2 Signer: ..."); the terse form lists "Signer #N certificate ...".
  const digests = [
    ...output.matchAll(
      /Signer[^\n]*?certificate SHA-256 digest: ([0-9a-fA-F:]+)/g,
    ),
  ].map((m) => normalizeDigest(m[1]));
  const declared = /Number of signers: (\d+)/.exec(output);
  const count = declared ? Number(declared[1]) : digests.length;
  if (count !== 1)
    throw new Error(`expected exactly one signer, found ${count}`);
  if (digests.length === 0)
    throw new Error("no signer certificate in apksigner output");
  const bad = digests.find((d) => d !== want);
  if (bad)
    throw new Error(
      `signing certificate ${bad} does not match the release certificate`,
    );
  return want;
}

export function findApksigner(env = process.env) {
  const root = env.ANDROID_HOME ?? env.ANDROID_SDK_ROOT;
  if (root && existsSync(join(root, "build-tools"))) {
    for (const v of readdirSync(join(root, "build-tools")).sort().reverse()) {
      const p = join(root, "build-tools", v, "apksigner");
      if (existsSync(p)) return p;
    }
  }
  return "apksigner";
}

if (import.meta.url === pathToFileURL(process.argv[1] ?? "").href) {
  const apk = process.argv[2];
  if (!apk) throw new Error("usage: verify-apk-cert.mjs <apk>");
  const here = dirname(fileURLToPath(import.meta.url));
  const expected =
    process.env.ANDROID_CERT_SHA256 ??
    readFileSync(join(here, "android-cert.sha256"), "utf8");
  const output = execFileSync(
    findApksigner(),
    ["verify", "--print-certs", "--verbose", apk],
    {
      encoding: "utf8",
    },
  );
  console.log(output.trim());
  console.log(`certificate verified: ${verifyCertOutput(output, expected)}`);
}
