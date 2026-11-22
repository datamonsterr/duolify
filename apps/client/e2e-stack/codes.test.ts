import { describe, expect, it } from "vitest";
import { waitForCode } from "./codes";

function clock() {
  let t = 0;
  return { now: () => t, sleep: async (ms: number) => void (t += ms) };
}

describe("waitForCode", () => {
  it("returns a code that arrives after a slow delivery", async () => {
    const c = clock();
    const list = async () =>
      c.now() >= 50_000 ? ["111111", "222222"] : ["111111"];
    await expect(waitForCode({ list, seen: 1, ...c })).resolves.toMatchObject({
      code: "222222",
      reused: false,
    });
  });

  it("fails with a bounded, explanatory error when no new mail arrives", async () => {
    const c = clock();
    await expect(
      waitForCode({ list: async () => ["111111"], seen: 1, ...c }),
    ).rejects.toThrow(/after 90s.*1 code\(s\), waiting for code 2.*60s/s);
    expect(c.now()).toBeLessThan(95_000);
  });

  it("accepts the still-valid code when the caller allows reuse", async () => {
    const c = clock();
    await expect(
      waitForCode({
        list: async () => ["111111"],
        seen: 1,
        reuse: true,
        timeoutMs: 5_000,
        ...c,
      }),
    ).resolves.toMatchObject({ code: "111111", reused: true });
  });

  it("never reuses without the flag", async () => {
    const c = clock();
    await expect(
      waitForCode({ list: async () => ["1"], seen: 1, timeoutMs: 2_000, ...c }),
    ).rejects.toThrow(/no sign-in code/);
  });
});
