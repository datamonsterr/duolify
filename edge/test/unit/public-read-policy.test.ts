import { describe, expect, it } from "vitest";
import {
  cacheKey,
  freshness,
  isEnabled,
  marketplaceTTL,
  publicReadSurface,
  publicReadMode,
} from "../../workers/public-read/policy";

const api = "https://api.example.test";
const origin = new URL("https://origin.example.test");
const market = "/v1/marketplace/courses";
const demo = "/v1/courses/shared-demo";

function key(path: string, headers?: HeadersInit) {
  const request = new Request(api + path, { headers });
  return cacheKey(request, origin, publicReadSurface(request));
}

describe("public read policy", () => {
  it("defaults to Hyperdrive and isolates cache entries by Marketplace read mode", async () => {
    expect(publicReadMode(undefined)).toBe("hyperdrive");
    expect(publicReadMode("hyperdrive")).toBe("hyperdrive");
    expect(publicReadMode("go_cache")).toBe("go_cache");
    expect(publicReadMode("unknown")).toBe("go_cache");
    const request = new Request(api + market);
    const primary = await cacheKey(
      request,
      origin,
      "marketplace",
      "hyperdrive",
    );
    const fallback = await cacheKey(request, origin, "marketplace", "go_cache");
    expect(primary).toBeTruthy();
    expect(fallback).toBeTruthy();
    expect(primary).not.toBe(fallback);
  });

  it("honours exact release flags and rejects invalid TTLs", () => {
    expect(isEnabled("true")).toBe(true);
    for (const off of [undefined, "", "false", "TRUE", "1"]) {
      expect(isEnabled(off)).toBe(false);
    }
    expect(marketplaceTTL("60")).toBe(60);
    for (const invalid of [undefined, "0", "-1", "3.4", "1e2", "3601"]) {
      expect(marketplaceTTL(invalid)).toBe(0);
    }
  });

  it("keys equivalent anonymous filters canonically without exposing values", async () => {
    const first = await key(market + "?q=rust%20query&sort=new", {
      "Accept-Language": "en",
    });
    const reordered = await key(market + "?sort=new&q=rust+query", {
      "Accept-Language": "en",
    });
    expect(first).toBeTruthy();
    expect(first).toBe(reordered);
    expect(first).not.toContain("rust");
    expect(first).not.toBe(
      await key(market + "?q=rust+query&sort=new", { "Accept-Language": "es" }),
    );
    expect(first).not.toBe(
      await cacheKey(
        new Request(api + market + "?sort=new&q=rust+query", {
          headers: { "Accept-Language": "en" },
        }),
        new URL("https://other-origin.example.test"),
        "marketplace",
      ),
    );
  });

  it("skips cookies, authorization, conditionals, ambiguous queries and share tokens", async () => {
    const credentialHeaders: HeadersInit[] = [
      { Cookie: "session=private" },
      { Authorization: "Bearer private" },
      { "If-None-Match": '"shared-demo-sky-v1"' },
      { "Cache-Control": "no-cache" },
      { Range: "bytes=0-100" },
    ];
    for (const headers of credentialHeaders) {
      expect(await key(market, headers)).toBeUndefined();
    }
    for (const query of [
      "?q=one&q=two",
      "?q=one;sort=new",
      "?q=%FF",
      "?unknown=x",
    ]) {
      expect(await key(market + query)).toBeUndefined();
    }
    expect(await key("/v1/public/shares/opaque-token")).toBeUndefined();
    expect(await key(market + "/id/reviews")).toBeUndefined();
  });

  it("caps Marketplace freshness from the origin read and requires its explicit opt-in", () => {
    const response = Response.json(
      {},
      {
        headers: {
          "Cache-Control": "no-store",
          "X-Duolify-Edge-Read-At": "100000",
          "X-Duolify-Edge-TTL": "60",
        },
      },
    );
    expect(freshness(response, "marketplace", 101000, 120, true)).toEqual({
      readAt: 100000,
      expiresAt: 160000,
    });
    expect(freshness(response, "course_page", 101000, 2, true)?.expiresAt).toBe(
      102000,
    );
    expect(
      freshness(response, "marketplace", 101000, 120, false),
    ).toBeUndefined();
    expect(
      freshness(
        Response.json({}, { headers: { "Cache-Control": "no-store" } }),
        "marketplace",
        101000,
        120,
        true,
      ),
    ).toBeUndefined();
  });

  it("preserves the immutable demo ETag contract and deducts origin age", async () => {
    expect(await key(demo)).toBeTruthy();
    const response = Response.json(
      {},
      {
        headers: {
          "Cache-Control": "public, max-age=300",
          ETag: '"shared-demo-sky-v1"',
          Age: "10",
        },
      },
    );
    expect(freshness(response, "shared_demo", 100000, 0, false)).toEqual({
      readAt: 90000,
      expiresAt: 390000,
    });
    expect(
      freshness(
        Response.json(
          {},
          { headers: { "Cache-Control": "public, max-age=300" } },
        ),
        "shared_demo",
        100000,
        0,
        false,
      ),
    ).toBeUndefined();
  });

  it("excludes Set-Cookie, private, no-store, share and non-success responses", () => {
    const demoHeaders = {
      ETag: '"shared-demo-sky-v1"',
      "Cache-Control": "public, max-age=300",
    };
    for (const response of [
      Response.json({}, { status: 404, headers: demoHeaders }),
      Response.json(
        {},
        { headers: { ...demoHeaders, "Set-Cookie": "session=private" } },
      ),
      Response.json(
        {},
        {
          headers: { ...demoHeaders, "Cache-Control": "private, max-age=300" },
        },
      ),
      Response.json(
        {},
        {
          headers: {
            ...demoHeaders,
            "Cache-Control": "public, no-store, max-age=300",
          },
        },
      ),
    ]) {
      expect(
        freshness(response, "shared_demo", 100000, 0, false),
      ).toBeUndefined();
    }
    expect(
      freshness(
        Response.json({}, { headers: demoHeaders }),
        "share_preview",
        100000,
        0,
        true,
      ),
    ).toBeUndefined();
  });
});
