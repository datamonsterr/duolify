import {
  createExecutionContext,
  waitOnExecutionContext,
} from "cloudflare:test";
import handler from "../../workers/public-read/index";
import { describe, expect, it, vi } from "vitest";
import {
  demo,
  env,
  fixture,
  market,
  origin,
  share,
  worker,
  workerInMode,
} from "../fixture";

const contractHeaders = [
  "cache-control",
  "content-type",
  "content-security-policy",
  "etag",
  "retry-after",
  "x-content-type-options",
  "x-robots-tag",
] as const;

async function compare(path: string, init?: RequestInit): Promise<Response> {
  const go = await origin(path, init);
  const edge = await worker(path, init);
  expect(edge.status, path).toBe(go.status);
  for (const header of contractHeaders) {
    expect(edge.headers.get(header), header).toBe(go.headers.get(header));
  }
  expect(await edge.clone().text()).toEqual(await go.text());
  return edge;
}

describe("public read conformance against seeded Go handlers", () => {
  it("matches payloads, security policy, errors and the versioned demo revalidation", async () => {
    for (const path of [
      demo,
      share,
      "/v1/public/shares/invalid",
      market,
      `${market}/${fixture.course}`,
      `${market}/${fixture.privateCourse}`,
      `${market}/invalid`,
      `${market}?cursor=invalid`,
      `${market}?limit=oops`,
    ]) {
      await compare(path);
    }
    const conditional = await compare(demo, {
      headers: { "If-None-Match": 'W/"shared-demo-sky-v1"' },
    });
    expect(conditional.status).toBe(304);
  });

  it("preserves filters, stable ordering and opaque cursor pagination", async () => {
    const path = `${market}?category=science&language=EN&sort=new&limit=1`;
    const first = await (
      await compare(path)
    ).json<{ items: unknown[]; next_cursor: string }>();
    expect(first.items).toHaveLength(1);
    expect(first.next_cursor).toBeTruthy();
    const next = await (
      await compare(`${path}&cursor=${encodeURIComponent(first.next_cursor)}`)
    ).json<{
      items: unknown[];
      next_cursor: string;
    }>();
    expect(next.items).toHaveLength(1);
    expect(next.items).not.toEqual(first.items);
    expect(next.next_cursor).toBe("");
    await compare(`${market}?q=+Astronomy+&sort=popular`);
    await compare(`${market}?category=invalid&category=science`);
    await compare(`${market}?q=astronomy;category=science`);
  });

  it("preserves the Go viewer response and denies malformed cache credentials", async () => {
    const path = `${market}/${fixture.course}`;
    const anonymous = await compare(path);
    expect(anonymous.status).toBe(200);
    await compare(path, { headers: { Cookie: "fixture_session=blocked" } });
    await compare(path, {
      headers: { Authorization: "Bearer fixture-blocked" },
    });

    const originHeaders = {
      "X-Duolify-Edge-Secret": "not-the-origin-secret",
      "X-Duolify-Edge-Cache-Valid-Until": String(Date.now() + 10_000),
    };
    const invalid = await origin(path, { headers: originHeaders });
    expect(invalid.status).toBe(200);
    expect(invalid.headers.get("X-Duolify-Edge-Read-At")).toBeNull();
    const forwarded = await worker(path, { headers: originHeaders });
    expect(forwarded.status).toBe(200);
    expect(forwarded.headers.get("X-Duolify-Edge-Read-At")).toBeNull();
  });

  it("requires trusted Go opt-in and origin freshness on cache fills", async () => {
    const path = `${market}/${fixture.course}`;
    const headers = {
      "X-Duolify-Edge-Secret": env.PUBLIC_READ_ORIGIN_SECRET,
      "X-Duolify-Edge-Client-IP": "192.0.2.3",
      "X-Duolify-Edge-Cache": "1",
    };
    const fresh = await origin(path, { headers });
    expect(fresh.status).toBe(200);
    expect(Number(fresh.headers.get("X-Duolify-Edge-Read-At"))).toBeGreaterThan(
      0,
    );
    expect(fresh.headers.get("X-Duolify-Edge-TTL")).toBe("2");

    const approved = await origin(path, {
      headers: {
        ...headers,
        "X-Duolify-Edge-Cache-Valid-Until": String(Date.now() + 2000),
        "X-Duolify-Edge-Read-At": fresh.headers.get("X-Duolify-Edge-Read-At")!,
      },
    });
    expect(approved.status).toBe(200);
    const expired = await origin(path, {
      headers: {
        ...headers,
        "X-Duolify-Edge-Cache-Valid-Until": String(Date.now() - 1),
        "X-Duolify-Edge-Read-At": fresh.headers.get("X-Duolify-Edge-Read-At")!,
      },
    });
    expect(expired.status).toBe(200);
  });

  it("rate limits warm reads at the edge and keeps caller IPs separate", async () => {
    const path = `${market}?category=arts`;
    const headers = { "CF-Connecting-IP": "192.0.2.21" };
    for (let hit = 0; hit < 8; hit++) {
      expect((await worker(path, { headers })).status).toBe(200);
    }
    const limit = await worker(path, { headers });
    expect(limit.status).toBe(429);
    expect(await limit.json()).toEqual({ error: "rate_limited" });
    expect(
      (await worker(path, { headers: { "CF-Connecting-IP": "192.0.2.22" } }))
        .status,
    ).toBe(200);
  });

  it("checks the Worker flag before cache lookup and preserves current CORS on a hit", async () => {
    const path = `${market}?category=life_skills`;
    const first = await worker(path, {
      headers: { Origin: "https://one.example.test" },
    });
    expect(first.status).toBe(200);
    const second = await compare(path, {
      headers: { Origin: "https://two.example.test" },
    });
    expect(second.headers.get("Access-Control-Allow-Origin")).toBe(
      "https://two.example.test",
    );
    expect(second.headers.get("Vary")).toBe("Origin");
    expect(second.headers.get("X-Request-ID")).not.toBe(
      first.headers.get("X-Request-ID"),
    );

    const fetchOrigin = vi.spyOn(globalThis, "fetch");
    const context = createExecutionContext();
    try {
      const request = new Request(`https://api.example.test${path}`, {
        headers: { Origin: "https://two.example.test" },
      });
      const bindings = {
        PUBLIC_READ_ORIGIN: env.PUBLIC_READ_ORIGIN,
        PUBLIC_READ_ORIGIN_SECRET: env.PUBLIC_READ_ORIGIN_SECRET,
        PUBLIC_READ_EDGE_ENABLED: "true",
        MARKETPLACE_ENABLED: "false",
        MARKETPLACE_CACHE_TTL_SECONDS: "2",
        API_ALLOWED_ORIGINS: "https://two.example.test",
      };
      const response = await handler.fetch(request, bindings, context);
      expect(response.status).toBe(404);
      expect(response.headers.get("Access-Control-Allow-Origin")).toBe(
        "https://two.example.test",
      );
      expect(await response.json()).toEqual({
        error: "marketplace_unavailable",
      });
      const forbidden = await handler.fetch(
        new Request(`https://api.example.test${path}`, {
          headers: { Origin: "https://other.example.test" },
        }),
        bindings,
        context,
      );
      expect(forbidden.status).toBe(403);
      expect(forbidden.headers.get("Access-Control-Allow-Origin")).toBeNull();
      expect(fetchOrigin).not.toHaveBeenCalled();
    } finally {
      fetchOrigin.mockRestore();
      await waitOnExecutionContext(context);
    }
  });

  it("serves fresh cache during an outage and refuses replay after expiry", async () => {
    const path = `${market}?category=languages`;
    expect((await workerInMode("go_cache", path)).status).toBe(200);
    const warmedAt = Date.now();
    const outbound = vi
      .spyOn(globalThis, "fetch")
      .mockRejectedValueOnce(new Error("synthetic origin failure"));
    try {
      expect((await workerInMode("go_cache", path)).status).toBe(200);
      expect(outbound).not.toHaveBeenCalled();
      const clock = vi.spyOn(Date, "now").mockReturnValue(warmedAt + 2000);
      try {
        const failed = await workerInMode("go_cache", path);
        expect(outbound).toHaveBeenCalledOnce();
        expect(failed.status).toBe(503);
        expect(failed.headers.get("Cache-Control")).toBe("no-store");
      } finally {
        clock.mockRestore();
      }
    } finally {
      outbound.mockRestore();
    }
  });
});
