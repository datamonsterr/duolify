import { describe, expect, it, vi } from "vitest";
import { fixture, market, origin, workerInMode } from "../fixture";

describe("Marketplace offload in workerd", () => {
  it("answers a cold Hyperdrive page and browse with no Go HTTP fetch", async () => {
    const paths = [`${market}/${fixture.course}`, `${market}?category=science`];
    const go = await Promise.all(
      paths.map(async (path) => (await origin(path)).json()),
    );
    const outbound = vi.spyOn(globalThis, "fetch");
    try {
      for (const [index, path] of paths.entries()) {
        const response = await workerInMode("hyperdrive", path);
        expect(response.status).toBe(200);
        expect(await response.json()).toEqual(go[index]);
      }
      expect(outbound).not.toHaveBeenCalled();
    } finally {
      outbound.mockRestore();
    }
  });

  it("serves warmed Go-cache pages and browse without contacting Go", async () => {
    const paths = [
      `${market}/${fixture.course}`,
      `${market}?language=en&sort=new`,
    ];
    const snapshots = await Promise.all(
      paths.map(async (path) => {
        const response = await workerInMode("go_cache", path);
        expect(response.status).toBe(200);
        return response.json();
      }),
    );
    const outbound = vi.spyOn(globalThis, "fetch");
    try {
      for (const [index, path] of paths.entries()) {
        const response = await workerInMode("go_cache", path);
        expect(response.status).toBe(200);
        expect(await response.json()).toEqual(snapshots[index]);
      }
      expect(outbound).not.toHaveBeenCalled();
    } finally {
      outbound.mockRestore();
    }
  });
});
