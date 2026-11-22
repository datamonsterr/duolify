import { expect, it } from "vitest";
import { parseSSOCallback, ssoBridge, ssoBridgeUrl } from "./sso";

it("accepts only the registered deep link and carries no secret", () => {
  const state = "s".repeat(43);
  expect(parseSSOCallback(`duolify://sso/callback?state=${state}`)).toEqual({
    state,
  });
  expect(parseSSOCallback("duolify://sso/callback?error=access_denied")).toBe(
    null,
  );
  for (const bad of [
    "https://evil.test/callback?state=x",
    "duolify://oauth/callback?state=x",
    "duolify://sso/other?state=x",
    "duolify://sso/callback",
    "not a url",
  ])
    expect(parseSSOCallback(bad)).toBeUndefined();
});

it("builds a same-origin bridge URL and refuses foreign browser targets", async () => {
  const url = new URL(ssoBridgeUrl("s".repeat(43), "oauth_google"));
  expect(url.origin).toBe(location.origin);
  expect(url.pathname).toBe("/sso-bridge");
  expect(url.searchParams.get("mode")).toBe("web");
  const bridge = await ssoBridge();
  await expect(
    bridge.openSystemBrowser("https://evil.test/sso-bridge?state=x"),
  ).rejects.toThrow("unsafe");
  await expect(
    bridge.openSystemBrowser(`${url.origin}/elsewhere`),
  ).rejects.toThrow("unsafe");
});
