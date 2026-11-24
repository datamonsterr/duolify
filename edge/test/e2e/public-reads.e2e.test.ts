import { describe, expect, it, vi } from "vitest";
import {
  action,
  demo,
  fixture,
  market,
  origin,
  revocable,
  share,
  worker,
  workerInMode,
} from "../fixture";

describe("public reads through workerd and the Go origin", () => {
  it("serves the authored shared demo, course page, link preview and browse", async () => {
    for (const path of [demo, `${market}/${fixture.course}`, share, market]) {
      const response = await worker(path);
      expect(response.status, path).toBe(200);
      expect(await response.json()).toEqual(await (await origin(path)).json());
    }
  });

  it("does not disclose private courses or content blocked for the signed-in viewer", async () => {
    expect((await worker(`${market}/${fixture.privateCourse}`)).status).toBe(
      404,
    );
    const path = `${market}/${fixture.course}`;
    expect((await worker(path)).status).toBe(200);
    const credentialHeaders: HeadersInit[] = [
      { Cookie: "fixture_session=blocked" },
      { Authorization: "Bearer fixture-blocked" },
    ];
    for (const headers of credentialHeaders) {
      expect((await worker(path, { headers })).status).toBe(404);
      expect((await worker(share, { headers })).status).toBe(404);
    }
  });

  it("checks rotated and revoked shares at the origin on the next open", async () => {
    expect((await worker(revocable)).status).toBe(200);
    const rotation = await action("rotate");
    expect(rotation.status).toBe(200);
    const rotated = await rotation.json<{ token: string }>();
    const current = `/v1/public/shares/${rotated.token}`;
    const old = await worker(revocable);
    expect(old.status).toBe(404);
    expect(await old.json()).toEqual({ error: "link_not_found" });
    expect((await worker(current)).status).toBe(200);
    expect((await action("revoke")).status).toBe(204);
    const response = await worker(current);
    expect(response.status).toBe(404);
    expect(await response.json()).toEqual({ error: "link_not_found" });
  });

  it("gates warmed reads with the Worker flag and checks Go on the next miss", async () => {
    const paths = [`${market}/${fixture.course}`, market];
    for (const path of paths) {
      expect((await workerInMode("go_cache", path)).status).toBe(200);
    }
    const warmedAt = Date.now();
    expect((await action("market-off")).status).toBe(204);
    try {
      for (const path of paths) {
        const off = await workerInMode("go_cache", path, undefined, {
          MARKETPLACE_ENABLED: "false",
        });
        expect(off.status).toBe(404);
        expect(await off.json()).toEqual({ error: "marketplace_unavailable" });
      }
      const clock = vi.spyOn(Date, "now").mockReturnValue(warmedAt + 2000);
      try {
        for (const path of paths) {
          const miss = await workerInMode("go_cache", path);
          expect(miss.status).toBe(404);
          expect(await miss.json()).toEqual({
            error: "marketplace_unavailable",
          });
        }
      } finally {
        clock.mockRestore();
      }
    } finally {
      await action("market-on");
    }
  });

  it("drops moderated content from both cached market surfaces by the TTL", async () => {
    const page = `${market}/${fixture.moderated}`;
    const browse = `${market}?q=moderationfixture`;
    for (const mode of ["go_cache", "hyperdrive"] as const) {
      expect((await workerInMode(mode, page)).status).toBe(200);
      expect(
        JSON.stringify(await (await workerInMode(mode, browse)).json()),
      ).toContain(fixture.moderated);
    }
    const warmedAt = Date.now();
    expect((await action("hide")).status).toBe(204);
    expect((await origin(page)).status).toBe(404);

    // Advance the Worker's freshness clock without coupling this assertion to Docker scheduling.
    const clock = vi.spyOn(Date, "now").mockReturnValue(warmedAt + 2000);
    try {
      for (const mode of ["go_cache", "hyperdrive"] as const) {
        const [course, listing] = await Promise.all([
          workerInMode(mode, page),
          workerInMode(mode, browse),
        ]);
        expect(course.status).toBe(404);
        expect(JSON.stringify(await listing.json())).not.toContain(
          fixture.moderated,
        );
      }
    } finally {
      clock.mockRestore();
    }
  });
});
