import { Client } from "pg";
import { describe, expect, it, vi } from "vitest";
import { action, env, fixture, market, origin, workerInMode } from "../fixture";

async function compare(path: string, status: number = 200) {
  const go = await origin(path);
  const outbound = vi.spyOn(globalThis, "fetch");
  let edge: Response;
  try {
    edge = await workerInMode("hyperdrive", path, undefined, {
      MARKETPLACE_CACHE_TTL_SECONDS: "0",
    });
    expect(outbound, path).not.toHaveBeenCalled();
  } finally {
    outbound.mockRestore();
  }
  expect(edge.status, path).toBe(status);
  expect(edge.status).toBe(go.status);
  for (const name of [
    "content-type",
    "cache-control",
    "x-robots-tag",
    "vary",
  ]) {
    expect(edge.headers.get(name), name).toBe(go.headers.get(name));
  }
  const payload = await edge.json();
  expect(payload).toEqual(await go.json());
  return payload as { items: { course_id: string }[]; next_cursor: string };
}

describe("Hyperdrive against Go and migrated Postgres", () => {
  it("matches Marketplace filters, ranking, cursors and public page fields", async () => {
    for (const sort of ["new", "rating", "popular"]) {
      const path = `${market}?category=science&language=EN&sort=${sort}&limit=1`;
      const first = await compare(path);
      expect(first.items).toHaveLength(1);
      expect(first.next_cursor).toBeTruthy();
      const next = await compare(`${path}&cursor=${first.next_cursor}`);
      expect(next.items).toHaveLength(1);
      expect(next.items[0]).not.toEqual(first.items[0]);
      expect(next.next_cursor).toBe("");
    }
    await compare(market);
    await compare(`${market}?q=+Astronomy+&sort=popular`);
    await compare(`${market}/${fixture.course}`);
    await compare(`${market}?cursor=invalid`, 400);
    await compare(`${market}?category=invalid`, 400);
    await compare(`${market}?limit=9223372036854775808`, 400);
  });

  it("matches private, missing, moderated and flag-off responses", async () => {
    await compare(`${market}/${fixture.privateCourse}`, 404);
    await compare(`${market}/00000000-0000-4000-8000-000000000001`, 404);
    expect((await action("hide-hyperdrive")).status).toBe(204);
    await compare(`${market}/${fixture.hyperdriveModerated}`, 404);
    expect(
      (await compare(`${market}?q=hyperdrivemoderationfixture`)).items,
    ).toEqual([]);
    expect((await action("market-off")).status).toBe(204);
    try {
      const response = await workerInMode("hyperdrive", market, undefined, {
        MARKETPLACE_ENABLED: "false",
      });
      expect(response.status).toBe(404);
      expect(await response.json()).toEqual(
        await (await origin(market)).json(),
      );
    } finally {
      await action("market-on");
    }
  });

  it("uses the exact read-only role and cannot read private tables or write", async () => {
    expect(env.MARKETPLACE_DB).toBeDefined();
    const client = new Client({
      connectionString: env.MARKETPLACE_DB.connectionString,
    });
    await client.connect();
    try {
      expect(
        (
          await client.query(
            "SELECT current_user AS role, current_setting('transaction_read_only') AS read_only",
          )
        ).rows,
      ).toEqual([{ role: "duolify_edge_ro", read_only: "on" }]);
      const allowed = await client.query(
        "SELECT outline, sample_slides FROM edge_public.marketplace_course_v1 WHERE course_id = $1",
        [fixture.course],
      );
      expect(allowed.rowCount).toBe(1);
      expect(JSON.stringify(allowed.rows[0])).not.toMatch(
        /SECRET-EXPLANATION|answer_index|narration|retry_pool/,
      );
      for (const table of [
        "learners",
        "courses",
        "publication_drafts",
        "publication_links",
        "auth_identities",
      ]) {
        await expect(
          client.query(`SELECT * FROM public.${table} LIMIT 1`),
        ).rejects.toMatchObject({ code: "42501" });
      }
      await client.query("SET default_transaction_read_only = off");
      await expect(
        client.query("DELETE FROM public.courses WHERE false"),
      ).rejects.toMatchObject({ code: "42501" });
      await expect(
        client.query("CREATE TEMP TABLE should_fail (id int)"),
      ).rejects.toMatchObject({ code: "42501" });
    } finally {
      // pg-cloudflare 1.4.1 can emit an error after a successful end().
      client.on("error", () => {});
      await client.end();
    }
  });

  it("falls back to the Go origin when the Hyperdrive connection fails", async () => {
    const path = `${market}?category=languages&sort=new`;
    const expected = await (await origin(path)).json();
    const outbound = vi.spyOn(globalThis, "fetch");
    try {
      const response = await workerInMode("hyperdrive", path, undefined, {
        MARKETPLACE_DB: {
          connectionString: "postgres://fixture@127.0.0.1:1/duolify",
        },
      });
      expect(response.status).toBe(200);
      expect(await response.json()).toEqual(expected);
      expect(outbound).toHaveBeenCalledOnce();
    } finally {
      outbound.mockRestore();
    }
  });
});
