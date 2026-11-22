// Reads the web security headers from infra/deploy/security-headers.caddy, the file Caddy ships with, so the
// policy the test server (`vite preview`) uses is the production one.
// Format of that file: snippets `(name) { header { Name "value" ... } }`, one header per line, values may hold
// {$VAR:default} placeholders (Caddy's environment syntax), filled from `env` here.
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import type { Plugin } from "vite";

export type Headers = Record<string, string>;
export type Surface = "web" | "sso" | "frame";

const SNIPPETS: Record<Surface, string[]> = {
  web: ["web_headers"],
  sso: ["web_headers", "sso_bridge_headers"],
  frame: ["lesson_frame_headers"],
};
export const FRAME_PATH = "/lesson-frame.html";
export const SSO_PATHS = ["/sso-bridge", "/sso-bridge/"];

// Run from apps/client (every client task does): the snippet sits in the repository's infra/deploy.
export const DEFAULT_SNIPPET_FILE = resolve(
  process.cwd(),
  "../../infra/deploy/security-headers.caddy",
);

export function parseSnippets(
  source: string,
  env: Record<string, string | undefined>,
): Record<string, Headers> {
  const fill = (value: string) =>
    value
      .replace(
        /\{\$([A-Z0-9_]+)(?::([^}]*))?\}/g,
        (_m, name: string, fallback = "") => env[name] ?? fallback,
      )
      .replace(/\s+/g, " ")
      .replace(/ ;/g, ";")
      .trim();
  const snippets: Record<string, Headers> = {};
  let current: Headers | undefined;
  for (const raw of source.split("\n")) {
    const line = raw.trim();
    if (line === "" || line.startsWith("#")) continue;
    const open = /^\((\w+)\)\s*\{$/.exec(line);
    if (open) {
      current = snippets[open[1]!] = {};
      continue;
    }
    const header = /^([A-Za-z][A-Za-z0-9-]*)\s+"(.*)"$/.exec(line);
    if (current && header) current[header[1]!] = fill(header[2]!);
  }
  return snippets;
}

export function headersFor(
  surface: Surface,
  env: Record<string, string | undefined> = process.env,
  file: string = DEFAULT_SNIPPET_FILE,
): Headers {
  const snippets = parseSnippets(readFileSync(file, "utf8"), env);
  const out: Headers = {};
  for (const name of SNIPPETS[surface]) {
    const found = snippets[name];
    if (!found) throw new Error(`security headers: snippet ${name} missing`);
    Object.assign(out, found);
  }
  return out;
}

export function surfaceOf(pathname: string): Surface {
  if (pathname === FRAME_PATH) return "frame";
  return SSO_PATHS.includes(pathname) ? "sso" : "web";
}

/**
 * `vite preview` sends the production headers, so browser tests run under the real policy. Opt in with
 * SECURITY_HEADERS=1; the {$VAR} names of the snippet are read from process.env.
 */
export function securityHeaders(): Plugin {
  return {
    name: "duolify-security-headers",
    configurePreviewServer(server) {
      server.middlewares.use((req, res, next) => {
        const { pathname } = new URL(req.url ?? "/", "http://x");
        // Always, as Caddy does: offline lesson playback replays the frame page from the HTTP cache.
        if (pathname === FRAME_PATH)
          res.setHeader("Cache-Control", "public, max-age=31536000, immutable");
        if (process.env.SECURITY_HEADERS === "1")
          for (const [name, value] of Object.entries(
            headersFor(surfaceOf(pathname)),
          ))
            res.setHeader(name, value);
        next();
      });
    },
  };
}
