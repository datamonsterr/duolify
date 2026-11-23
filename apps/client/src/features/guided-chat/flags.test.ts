import { afterEach, describe, expect, it, vi } from "vitest";
import { guidedEnabled } from "./flags";

afterEach(() => {
  vi.unstubAllEnvs();
  localStorage.clear();
});
describe("guided flags", () => {
  it("is on by default", () => expect(guidedEnabled("onboarding")).toBe(true));
  it("env false turns it off", () => {
    vi.stubEnv("VITE_FEATURE_GUIDED_ONBOARDING", "false");
    expect(guidedEnabled("onboarding")).toBe(false);
  });
  it("ab splits by learner id, stably", () => {
    vi.stubEnv("VITE_FEATURE_GUIDED_ONBOARDING", "ab");
    const ids = Array.from({ length: 40 }, (_, i) => `learner-${i}`);
    const on = ids.filter((id) => guidedEnabled("onboarding", id));
    expect(on.length).toBeGreaterThan(8);
    expect(on.length).toBeLessThan(32);
    expect(guidedEnabled("onboarding", ids[0])).toBe(
      guidedEnabled("onboarding", ids[0]),
    );
    expect(guidedEnabled("onboarding")).toBe(false);
  });
  it("a local override wins", () => {
    vi.stubEnv("VITE_FEATURE_GUIDED_ONBOARDING", "false");
    localStorage.setItem("duolify.guided.onboarding", "on");
    expect(guidedEnabled("onboarding")).toBe(true);
  });
});
