import { afterEach, expect, it, vi } from "vitest";
import tauriConfig from "../../../src-tauri/tauri.conf.json";

vi.mock("@tauri-apps/api/core", () => ({ isTauri: () => false }));

afterEach(() => {
  vi.unstubAllEnvs();
  vi.restoreAllMocks();
  vi.resetModules();
});

it("rejects fixture hosts in production while allowing real provider hosts", async () => {
  vi.stubEnv("DEV", false);
  const open = vi.spyOn(window, "open").mockReturnValue(null);
  const { oauthBridge } = await import("./google-oauth");
  for (const [provider, real, fixture] of [
    ["google", "accounts.google.com", "accounts.google.test"],
    ["apple", "appleid.apple.com", "appleid.apple.test"],
  ] as const) {
    const bridge = await oauthBridge(provider);
    for (const url of [
      `https://${fixture}/authorize`,
      `http://${real}/authorize`,
      `https://${real}.evil.test/authorize`,
    ]) {
      await expect(bridge.openSystemBrowser(url)).rejects.toThrow(
        "unsafe browser URL",
      );
    }
    await bridge.openSystemBrowser(`https://${real}/authorize`);
  }
  expect(open).toHaveBeenCalledTimes(2);
});

it("does not allow inline scripts in the Tauri CSP", () => {
  const directive = tauriConfig.app.security.csp
    .split(";")
    .find((value) => value.trim().startsWith("script-src "));
  expect(directive).toBeDefined();
  expect(directive).not.toContain("'unsafe-inline'");
});
