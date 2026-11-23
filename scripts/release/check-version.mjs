// A release tag vX.Y.Z must match every client version declaration.
import { readFileSync } from "node:fs";
import { pathToFileURL } from "node:url";

export function checkVersions(tag, versions) {
  const m = /^v(\d+\.\d+\.\d+)$/.exec(tag);
  if (!m) throw new Error(`tag ${tag} is not vMAJOR.MINOR.PATCH`);
  for (const [file, version] of Object.entries(versions))
    if (version !== m[1])
      throw new Error(`${file} is ${version}, tag wants ${m[1]}`);
  return m[1];
}

if (import.meta.url === pathToFileURL(process.argv[1] ?? "").href) {
  const tag = process.argv[2];
  const cargo = /^version = "([^"]+)"/m.exec(
    readFileSync("apps/client/src-tauri/Cargo.toml", "utf8"),
  )?.[1];
  const json = (f) => JSON.parse(readFileSync(f, "utf8")).version;
  console.log(
    checkVersions(tag ?? "", {
      "apps/client/package.json": json("apps/client/package.json"),
      "apps/client/src-tauri/tauri.conf.json": json(
        "apps/client/src-tauri/tauri.conf.json",
      ),
      "apps/client/src-tauri/Cargo.toml": cargo,
    }),
  );
}
