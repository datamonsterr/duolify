import { afterEach, describe, expect, it, vi } from "vitest";
import { isTouchDevice } from "./entry";

afterEach(() => vi.unstubAllEnvs());

describe("isTouchDevice", () => {
  it("a coarse pointer or a hover-less device is a touch device", () => {
    expect(isTouchDevice(true, false)).toBe(true);
    expect(isTouchDevice(false, true)).toBe(true);
  });
  it("a desktop window is not", () => {
    expect(isTouchDevice(false, false)).toBe(false);
  });
  it("the native phone app is, even when its WebView reports a fine pointer", () => {
    vi.stubEnv("VITE_MOBILE_PLATFORM", "android");
    expect(isTouchDevice(false, false)).toBe(true);
    vi.stubEnv("VITE_MOBILE_PLATFORM", "ios");
    expect(isTouchDevice(false, false)).toBe(true);
    vi.stubEnv("VITE_MOBILE_PLATFORM", "");
    expect(isTouchDevice(false, false)).toBe(false);
  });
});
