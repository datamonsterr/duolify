import { describe, expect, it } from "vitest";
import { headersFor, parseSnippets, surfaceOf } from "./securityHeaders.ts";

const env = {
  LESSON_ASSET_ORIGIN: "https://assets.example.test",
  POSTHOG_HOST: "https://eu.i.posthog.com",
  CLERK_FAPI_ORIGIN: "https://clerk.example.test",
};
const directives = (csp: string) =>
  Object.fromEntries(
    csp
      .split(";")
      .map((d) => d.trim().split(/\s+/))
      .filter((d) => d[0])
      .map(([name, ...sources]) => [name, sources]),
  );

describe("security headers (infra/deploy/security-headers.caddy)", () => {
  it("fills placeholders from the environment and drops empty ones", () => {
    const web = headersFor("web", env);
    const csp = directives(web["Content-Security-Policy"]!);
    expect(csp["connect-src"]).toEqual([
      "'self'",
      "https://assets.example.test",
      "https://eu.i.posthog.com",
    ]);
    expect(web["Content-Security-Policy"]).not.toContain("{$");
    expect(web["Content-Security-Policy"]).not.toMatch(/\s{2}|\s;/);
  });

  it("falls back to the documented defaults, not to a hostname of ours", () => {
    const csp = headersFor("web", {})["Content-Security-Policy"]!;
    expect(directives(csp)["connect-src"]).toEqual([
      "'self'",
      "http://localhost:9000",
      "https://us.i.posthog.com",
    ]);
    expect(csp).not.toMatch(/duolify\.online/);
  });

  it("web policy: strict, no eval, no inline script, no framing", () => {
    const web = headersFor("web", env);
    const csp = directives(web["Content-Security-Policy"]!);
    expect(csp["default-src"]).toEqual(["'self'"]);
    expect(csp["script-src"]).toEqual(["'self'"]);
    expect(web["Content-Security-Policy"]).not.toContain("unsafe-eval");
    expect(csp["frame-ancestors"]).toEqual(["'none'"]);
    expect(csp["object-src"]).toEqual(["'none'"]);
    expect(csp["base-uri"]).toEqual(["'self'"]);
    expect(csp["form-action"]).toEqual(["'self'"]);
    expect(csp["frame-src"]).toEqual(["'self'"]);
    for (const d of ["img-src", "media-src", "connect-src"])
      expect(csp[d]).toContain("https://assets.example.test");
    expect(web["Strict-Transport-Security"]).toBe(
      "max-age=31536000; includeSubDomains",
    );
    expect(web["X-Content-Type-Options"]).toBe("nosniff");
    expect(web["Referrer-Policy"]).toBe("strict-origin-when-cross-origin");
    expect(web["Permissions-Policy"]).toContain("microphone=(self)");
    expect(web["Permissions-Policy"]).toContain("camera=()");
    expect(web["Permissions-Policy"]).toContain("geolocation=()");
  });

  it("only the SSO page may talk to Clerk", () => {
    const web = directives(headersFor("web", env)["Content-Security-Policy"]!);
    const sso = headersFor("sso", env);
    const csp = directives(sso["Content-Security-Policy"]!);
    expect(JSON.stringify(web)).not.toContain("clerk");
    expect(csp["script-src"]).toContain("https://clerk.example.test");
    expect(csp["connect-src"]).toContain("https://clerk.example.test");
    expect(csp["frame-src"]).toContain("https://challenges.cloudflare.com");
    expect(csp["frame-ancestors"]).toEqual(["'none'"]);
    expect(sso["Strict-Transport-Security"]).toBeDefined();
  });

  it("lesson frame: inline code but network only to the asset origin", () => {
    const frame = headersFor("frame", env);
    const csp = directives(frame["Content-Security-Policy"]!);
    expect(csp["default-src"]).toEqual(["'none'"]);
    expect(csp["script-src"]).toContain("'unsafe-inline'");
    expect(csp["connect-src"]).toEqual(["https://assets.example.test"]);
    expect(csp["frame-src"]).toEqual(["'none'"]);
    expect(csp["frame-ancestors"]).toEqual(["'self'"]);
    expect(frame["Content-Security-Policy"]).not.toContain("unsafe-eval");
    expect(frame["Permissions-Policy"]).toContain("microphone=()");
  });

  it("routes paths to the right surface", () => {
    expect(surfaceOf("/lesson-frame.html")).toBe("frame");
    expect(surfaceOf("/sso-bridge")).toBe("sso");
    expect(surfaceOf("/courses/1")).toBe("web");
  });

  it("ignores comments and unknown lines", () => {
    expect(
      parseSnippets('# x\n(a) {\n\theader {\n\t\tX-A "1 {$B:2}"\n\t}\n}\n', {}),
    ).toEqual({ a: { "X-A": "1 2" } });
  });
});
