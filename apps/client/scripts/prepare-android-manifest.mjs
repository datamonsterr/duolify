import { readFileSync, writeFileSync } from "node:fs";

const file = "src-tauri/gen/android/app/src/main/AndroidManifest.xml";
let manifest = readFileSync(file, "utf8");
for (const permission of ["RECORD_AUDIO", "MODIFY_AUDIO_SETTINGS"]) {
  if (manifest.includes(`android.permission.${permission}`)) continue;
  const insertion = `<uses-permission android:name="android.permission.${permission}" />\n    `;
  if (!manifest.includes("<application"))
    throw new Error("Android manifest has no application element");
  manifest = manifest.replace("<application", `${insertion}<application`);
}
writeFileSync(file, manifest);
