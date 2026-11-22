import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { http, HttpResponse } from "msw";
import { server } from "../../test/setup";

// Cookie mode needs the API on the page origin: the deployed layout builds with
// VITE_API_URL="". Each test imports a fresh module so `base` is re-evaluated.
const profile = {
  id: "56d4e614-7665-4f14-b83e-8b5e9bed384a",
  display_name: "Learner",
  analytics_opt_out: false,
};
async function fresh(apiUrl: string) {
  vi.resetModules();
  vi.stubEnv("VITE_API_URL", apiUrl);
  return import("./session");
}
beforeEach(() => {
  localStorage.clear();
  sessionStorage.clear();
});
afterEach(() => {
  vi.unstubAllEnvs();
});

it("restores the session after a reload from the refresh cookie, with no body and the web header", async () => {
  const { session, cookieMode } = await fresh("");
  expect(cookieMode()).toBe(true);
  let refreshHeaders: Headers | undefined;
  let refreshBody = "unset";
  server.use(
    http.post("*/v1/auth/refresh", async ({ request }) => {
      refreshHeaders = request.headers;
      refreshBody = await request.text();
      return HttpResponse.json({ access_token: "minted", expires_in: 900 });
    }),
    http.get("*/v1/me", ({ request }) =>
      request.headers.get("Authorization") === "Bearer minted"
        ? HttpResponse.json(profile)
        : HttpResponse.json({}, { status: 401 }),
    ),
  );
  await expect(session.load()).resolves.toMatchObject({ id: profile.id });
  expect(refreshHeaders?.get("X-Duolify-Client")).toBe("web");
  expect(refreshBody).toBe("");
  // Script never sees or stores a refresh credential; the access token stays in memory.
  const stored = JSON.stringify({ ...localStorage, ...sessionStorage });
  expect(stored).not.toContain("minted");
  expect(stored).not.toContain("refresh_token");
  session.clear();
});

it("treats a rejected cookie as signed out, once, without a thrown error", async () => {
  const { session } = await fresh("");
  let refreshes = 0;
  server.use(
    http.post("*/v1/auth/refresh", () => {
      refreshes++;
      return HttpResponse.json({ error: "no" }, { status: 401 });
    }),
  );
  await expect(session.load()).resolves.toBeNull();
  await expect(session.load()).resolves.toBeNull();
  expect(refreshes).toBe(1);
});

it("an offline first load throws instead of reporting a signed-out learner", async () => {
  const { session } = await fresh("");
  server.use(http.post("*/v1/auth/refresh", () => HttpResponse.error()));
  await expect(session.load()).rejects.toBeInstanceOf(TypeError);
});

it("signs in without a refresh token and logs out through the cookie", async () => {
  const { session, credentialStore } = await fresh("");
  const calls: { path: string; body: string; client: string | null }[] = [];
  server.use(
    http.get("*/v1/me", () => HttpResponse.json(profile)),
    http.post("*/v1/auth/logout", async ({ request }) => {
      calls.push({
        path: "logout",
        body: await request.text(),
        client: request.headers.get("X-Duolify-Client"),
      });
      return new HttpResponse(null, { status: 204 });
    }),
  );
  await session.accept({
    access_token: "a",
    refresh_token: undefined as unknown as string,
    expires_in: 900,
  });
  expect((await credentialStore.read())?.refresh_token).toBe("");
  expect(localStorage.getItem("duolify.offline-identity")).toBeNull();
  await session.logout();
  expect(calls).toEqual([{ path: "logout", body: "", client: "web" }]);
  expect(await credentialStore.read()).toBeNull();
});

it("rotates an expired access token through the cookie and shares one refresh", async () => {
  const { session, credentialStore } = await fresh("");
  await credentialStore.write({
    access_token: "old",
    refresh_token: "",
    expires_in: 900,
  });
  let refreshes = 0;
  server.use(
    http.get("*/v1/thing", ({ request }) =>
      request.headers.get("Authorization") === "Bearer new"
        ? HttpResponse.json({ ok: true })
        : HttpResponse.json({}, { status: 401 }),
    ),
    http.post("*/v1/auth/refresh", async ({ request }) => {
      refreshes++;
      expect(await request.text()).toBe("");
      return HttpResponse.json({ access_token: "new", expires_in: 900 });
    }),
  );
  await Promise.all([
    session.request("/v1/thing"),
    session.request("/v1/thing"),
  ]);
  expect(refreshes).toBe(1);
  session.clear();
  await credentialStore.write(null);
});

it("keeps the body flow on a cross-origin build and sends no web header", async () => {
  const { session, cookieMode } = await fresh("http://localhost:8080");
  expect(cookieMode()).toBe(false);
  let sawHeader: string | null = "unset";
  server.use(
    http.get("*/v1/me", ({ request }) => {
      sawHeader = request.headers.get("X-Duolify-Client");
      return HttpResponse.json(profile);
    }),
  );
  await session.accept({
    access_token: "a",
    refresh_token: "r",
    expires_in: 900,
  });
  expect(sawHeader).toBeNull();
  session.clear();
});
