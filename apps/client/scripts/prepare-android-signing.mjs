// Release-only Android signing. Patches the generated app/build.gradle.kts so
// only the release build type reads the keystore; debug builds stay unsigned by
// it. Credentials come from the environment and are written outside the repo.
import process from "node:process";
import { readFileSync, writeFileSync } from "node:fs";
import { pathToFileURL } from "node:url";

const SIGNING = `signingConfigs {
        create("release") {
            val props = Properties()
            props.load(FileInputStream(rootProject.file("keystore.properties")))
            keyAlias = props["keyAlias"] as String
            keyPassword = props["keyPassword"] as String
            storeFile = file(props["storeFile"] as String)
            storePassword = props["storePassword"] as String
        }
    }
    `;

export function patchGradle(source) {
  if (source.includes('signingConfigs.getByName("release")')) return source;
  if (
    !/\n\s*buildTypes\s*\{/.test(source) ||
    !/getByName\("release"\)\s*\{/.test(source)
  )
    throw new Error("build.gradle.kts has no buildTypes release block to sign");
  // The generated file may already import Properties; a duplicate is an error.
  const missing = ["java.io.FileInputStream", "java.util.Properties"]
    .filter((name) => !source.includes(`import ${name}`))
    .map((name) => `import ${name}\n`)
    .join("");
  const withImports = `${missing}${source}`;
  return withImports
    .replace(/(\n\s*)buildTypes\s*\{/, (m, ws) => `${ws}${SIGNING}buildTypes {`)
    .replace(
      /getByName\("release"\)\s*\{/,
      (m) =>
        `${m}\n            signingConfig = signingConfigs.getByName("release")`,
    );
}

export function keystoreProperties(env, storeFile) {
  const need = [
    "ANDROID_KEY_ALIAS",
    "ANDROID_KEYSTORE_PASSWORD",
    "ANDROID_KEY_PASSWORD",
  ];
  const missing = need.filter((k) => !env[k]);
  if (missing.length || !storeFile)
    throw new Error(`missing Android signing inputs: ${missing.join(", ")}`);
  return `keyAlias=${env.ANDROID_KEY_ALIAS}\nkeyPassword=${env.ANDROID_KEY_PASSWORD}\nstorePassword=${env.ANDROID_KEYSTORE_PASSWORD}\nstoreFile=${storeFile}\n`;
}

if (import.meta.url === pathToFileURL(process.argv[1] ?? "").href) {
  const [keystore] = process.argv.slice(2);
  const dir = "src-tauri/gen/android";
  writeFileSync(
    `${dir}/keystore.properties`,
    keystoreProperties(process.env, keystore),
  );
  const gradle = `${dir}/app/build.gradle.kts`;
  writeFileSync(gradle, patchGradle(readFileSync(gradle, "utf8")));
}
