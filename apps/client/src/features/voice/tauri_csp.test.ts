import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

// Native builds reach the API (voice uploads included) only through the webview's connect-src.
describe("Tauri content security policy", () => {
  const conf = JSON.parse(
    readFileSync(
      resolve(__dirname, "../../../src-tauri/tauri.conf.json"),
      "utf8",
    ),
  );
  const csp: string = conf.app.security.csp;
  const connect = csp
    .split(";")
    .map((d) => d.trim())
    .find((d) => d.startsWith("connect-src "))!
    .split(/\s+/)
    .slice(1);

  it("lets native builds call the production and staging API origins", () => {
    for (const origin of [
      "https://duolify.online",
      "https://api.duolify.online",
      "https://staging.duolify.online",
      "https://staging-api.duolify.online",
    ]) {
      expect(connect).toContain(origin);
    }
  });

  it("stays narrow: no wildcard host, no blanket scheme, scripts still self only", () => {
    for (const source of connect) {
      expect(source).not.toBe("*");
      expect(source).not.toMatch(/^(https?|wss?):$/);
    }
    expect(csp).toContain("script-src 'self';");
    expect(csp).toContain("object-src 'none'");
  });
});
