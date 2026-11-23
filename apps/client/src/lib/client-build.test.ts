import { describe, expect, it } from "vitest";
import { clientPlatform } from "./client-build";

describe("client build identity", () => {
  it("reports web for browser sessions and the mobile target for native sessions", () => {
    expect(clientPlatform(false, "android")).toBe("web");
    expect(clientPlatform(true, "android")).toBe("android");
    expect(clientPlatform(true, "ios")).toBe("ios");
    expect(clientPlatform(true, undefined)).toBe("desktop");
  });
});
