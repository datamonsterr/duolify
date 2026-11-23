// Publishing is only allowed for commits reachable from origin/main.
import { spawnSync } from "node:child_process";
import { pathToFileURL } from "node:url";

export function isOnMain(sha, { cwd, ref = "origin/main" } = {}) {
  if (!/^[0-9a-f]{40}$/.test(sha ?? ""))
    throw new Error(`bad commit sha: ${sha}`);
  const r = spawnSync("git", ["merge-base", "--is-ancestor", sha, ref], {
    cwd,
  });
  if (r.error) throw r.error;
  if (r.status === 0) return true;
  if (r.status === 1) return false;
  throw new Error(`cannot resolve ${ref}: ${String(r.stderr).trim()}`);
}

if (import.meta.url === pathToFileURL(process.argv[1] ?? "").href) {
  const [sha] = process.argv.slice(2);
  if (!isOnMain(sha)) {
    console.error(
      `::error::${sha} is not on origin/main; refusing to publish a release from another ref`,
    );
    process.exit(1);
  }
  console.log(`${sha} is on origin/main`);
}
