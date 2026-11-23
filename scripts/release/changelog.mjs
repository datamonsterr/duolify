// Changelog from Conventional Commits between two tags.
import { execFileSync } from "node:child_process";
import { writeFileSync } from "node:fs";
import { pathToFileURL } from "node:url";

const SECTIONS = [
  ["feat", "Features"],
  ["fix", "Fixes"],
  ["perf", "Performance"],
  ["refactor", "Refactors"],
  ["docs", "Documentation"],
  ["test", "Tests"],
  ["ci", "CI"],
  ["chore", "Chores"],
];

export function parseSubject(subject) {
  const m = /^(\w+)(?:\(([^)]+)\))?(!)?: (.+)$/.exec(subject.trim());
  if (!m) return null;
  return { type: m[1], scope: m[2] ?? "", breaking: Boolean(m[3]), text: m[4] };
}

export function renderChangelog({ version, previous, commits, repo }) {
  const groups = new Map();
  const breaking = [];
  for (const { sha, subject } of commits) {
    if (/^Merge /.test(subject)) continue;
    const parsed = parseSubject(subject);
    const entry = parsed ?? {
      type: "other",
      scope: "",
      breaking: false,
      text: subject,
    };
    const line = `- ${entry.scope ? `**${entry.scope}:** ` : ""}${entry.text} (${sha.slice(0, 7)})`;
    if (entry.breaking) breaking.push(line);
    const known = SECTIONS.some(([t]) => t === entry.type);
    const key = known ? entry.type : "other";
    groups.set(key, [...(groups.get(key) ?? []), line]);
  }
  const out = [`## ${version}`, ""];
  if (breaking.length) out.push("### Breaking changes", ...breaking, "");
  for (const [type, title] of [...SECTIONS, ["other", "Other changes"]]) {
    const lines = groups.get(type);
    if (lines) out.push(`### ${title}`, ...lines, "");
  }
  if (out.length === 2) out.push("No user-facing changes.", "");
  if (previous && repo)
    out.push(
      `Full diff: https://github.com/${repo}/compare/${previous}...${version}`,
      "",
    );
  return out.join("\n");
}

function git(...args) {
  return execFileSync("git", args, { encoding: "utf8" }).trim();
}

export function previousTag(tag) {
  const tags = git("tag", "--list", "v*", "--sort=-v:refname")
    .split("\n")
    .filter(Boolean)
    .filter((t) => t !== tag);
  const own = git("tag", "--list", tag) ? tag : "HEAD";
  const reachable = new Set(git("tag", "--merged", own).split("\n"));
  return tags.find((t) => reachable.has(t)) ?? "";
}

if (import.meta.url === pathToFileURL(process.argv[1] ?? "").href) {
  const [tag, outFile] = process.argv.slice(2);
  if (!tag) throw new Error("usage: changelog.mjs <tag> [out-file]");
  const head = git("tag", "--list", tag) ? tag : "HEAD";
  const previous = previousTag(tag);
  const range = previous ? `${previous}..${head}` : head;
  // First release has no previous tag: keep the notes inside GitHub's body limit.
  const cap = previous ? [] : ["--max-count=150"];
  const log = git("log", "--no-merges", ...cap, "--format=%H%x09%s", range);
  const commits = log
    .split("\n")
    .filter(Boolean)
    .map((l) => {
      const [sha, ...rest] = l.split("\t");
      return { sha, subject: rest.join("\t") };
    });
  const md = renderChangelog({
    version: tag,
    previous,
    commits,
    repo: process.env.GITHUB_REPOSITORY,
  });
  if (outFile) writeFileSync(outFile, md);
  else process.stdout.write(md);
}
