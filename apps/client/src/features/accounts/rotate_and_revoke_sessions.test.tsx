import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { http, HttpResponse } from "msw";
import { server } from "../../test/setup";
import { session, credentialStore } from "./session";
import { Sessions } from "./Sessions";
import { RouteGuard } from "./RouteGuard";
import { configureAnalytics, trackSessionRecovery } from "../../lib/analytics";
const posthog = vi.hoisted(() => ({
  capture: vi.fn(),
  has_opted_out_capturing: vi.fn(() => false),
  identify: vi.fn(),
  init: vi.fn(),
}));
vi.mock("posthog-js/dist/module.no-external", () => ({ default: posthog }));
const id = "56d4e614-7665-4f14-b83e-8b5e9bed384a";
const profile = { id, display_name: "Learner", analytics_opt_out: false };
const tokens = {
  access_token: "old",
  refresh_token: "private-refresh",
  expires_in: 900,
};
afterEach(async () => {
  session.clear();
  await credentialStore.write(null);
  configureAnalytics(null);
  vi.clearAllMocks();
});
it("shares refresh across protected mutations and retries each once", async () => {
  await credentialStore.write(tokens);
  let refreshes = 0;
  server.use(
    http.delete("*/v1/auth/sessions/device", ({ request }) =>
      request.headers.get("Authorization") === "Bearer fresh"
        ? new HttpResponse(null, { status: 204 })
        : HttpResponse.json({}, { status: 401 }),
    ),
    http.post("*/v1/auth/refresh", () => {
      refreshes++;
      return HttpResponse.json({
        ...tokens,
        access_token: "fresh",
        refresh_token: "rotated",
      });
    }),
  );
  await Promise.all([
    session.request("/v1/auth/sessions/device", "DELETE"),
    session.request("/v1/auth/sessions/device", "DELETE"),
  ]);
  expect(refreshes).toBe(1);
  expect((await credentialStore.read())?.refresh_token).toBe("rotated");
});
it("keeps credentials on offline refresh and never retries a rejected fresh token forever", async () => {
  await credentialStore.write(tokens);
  let attempts = 0;
  server.use(
    http.get("*/v1/auth/sessions", () => {
      attempts++;
      return HttpResponse.json({}, { status: 401 });
    }),
    http.post("*/v1/auth/refresh", () => HttpResponse.error()),
  );
  await expect(session.request("/v1/auth/sessions")).rejects.toThrow();
  expect(await credentialStore.read()).toEqual(tokens);
  server.use(
    http.post("*/v1/auth/refresh", () =>
      HttpResponse.json({ ...tokens, access_token: "fresh" }),
    ),
  );
  await expect(session.request("/v1/auth/sessions")).rejects.toThrow();
  expect(attempts).toBe(3);
  expect(await credentialStore.read()).toBeNull();
  expect(session.expired()).toBe(true);
});
it("does not resurrect credentials when logout overtakes refresh", async () => {
  await credentialStore.write(tokens);
  let release!: () => void;
  const held = new Promise<void>((resolve) => {
    release = resolve;
  });
  let refreshing = false;
  server.use(
    http.get("*/v1/auth/sessions", () =>
      HttpResponse.json({}, { status: 401 }),
    ),
    http.post("*/v1/auth/refresh", async () => {
      refreshing = true;
      await held;
      return HttpResponse.json({ ...tokens, access_token: "fresh" });
    }),
    http.post(
      "*/v1/auth/logout",
      () => new HttpResponse(null, { status: 204 }),
    ),
  );
  const pending = session.request("/v1/auth/sessions").catch(() => null);
  await waitFor(() => expect(refreshing).toBe(true));
  await session.logout();
  release();
  await pending;
  expect(await credentialStore.read()).toBeNull();
  expect(session.current()).toBeNull();
});
it("lists devices, retries errors and revokes an owned device", async () => {
  await credentialStore.write(tokens);
  let failed = true,
    revoked = false;
  server.use(
    http.get("*/v1/auth/sessions", () =>
      failed
        ? HttpResponse.json({}, { status: 503 })
        : HttpResponse.json(
            revoked
              ? []
              : [
                  {
                    id: "device",
                    current: false,
                    created_at: "2026-11-09T00:00:00Z",
                    expires_at: "2026-12-09T00:00:00Z",
                  },
                ],
          ),
    ),
    http.delete("*/v1/auth/sessions/device", () => {
      revoked = true;
      return new HttpResponse(null, { status: 204 });
    }),
  );
  render(<Sessions />);
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "Connect and retry",
  );
  failed = false;
  fireEvent.click(screen.getByRole("button", { name: "Retry" }));
  fireEvent.click(
    await screen.findByRole("button", { name: "Sign out device" }),
  );
  expect(await screen.findByText("No active sessions.")).toBeVisible();
});
it("recovers in place without unmounting private drafts and emits only catalogued properties", async () => {
  await credentialStore.write(tokens);
  let active = true;
  server.use(
    http.get("*/v1/me", () =>
      active
        ? HttpResponse.json(profile)
        : HttpResponse.json({}, { status: 401 }),
    ),
    http.post("*/v1/auth/refresh", () =>
      HttpResponse.json({}, { status: 401 }),
    ),
    http.post("*/v1/auth/code", () => HttpResponse.json({}, { status: 202 })),
    http.post("*/v1/auth/verify", () => HttpResponse.json(tokens)),
  );
  render(
    <RouteGuard>
      <label>
        Draft
        <input defaultValue="private draft" />
      </label>
    </RouteGuard>,
  );
  const draft = await screen.findByLabelText("Draft");
  fireEvent.change(draft, { target: { value: "private unsaved topic" } });
  active = false;
  await session.load();
  expect(
    await screen.findByRole("button", { name: "Sign in again" }),
  ).toBeVisible();
  expect(draft.closest("[hidden]")).not.toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Sign in again" }));
  expect(await screen.findByLabelText("Email address")).toHaveFocus();
  expect(posthog.capture).toHaveBeenCalledWith("ui_recovery_selected", {
    distinct_id: id,
    platform: "web",
    app_version: "0.1.0",
    source: "client",
    surface: "sign_in",
    error_code: "session_expired",
    action: "sign_in",
  });
  fireEvent.change(screen.getByLabelText("Email address"), {
    target: { value: "private@example.test" },
  });
  fireEvent.click(screen.getByLabelText("I am 16 or older"));
  fireEvent.click(screen.getByRole("button", { name: "Send code" }));
  fireEvent.change(await screen.findByLabelText("One-time code"), {
    target: { value: "123456" },
  });
  active = true;
  fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
  await waitFor(() => expect(draft.closest("[hidden]")).toBeNull());
  expect(screen.getByLabelText("Draft")).toBe(draft);
  expect(draft).toHaveValue("private unsaved topic");
  expect(JSON.stringify(posthog.capture.mock.calls)).not.toMatch(
    /private|123456|@/,
  );
  configureAnalytics({ learnerId: id, platform: "web", optedOut: true });
  posthog.capture.mockClear();
  trackSessionRecovery();
  expect(posthog.capture).not.toHaveBeenCalled();
  configureAnalytics({ learnerId: id, platform: "web", optedOut: false });
  posthog.has_opted_out_capturing.mockReturnValueOnce(true);
  trackSessionRecovery();
  expect(posthog.capture).not.toHaveBeenCalled();
});

it("discards a previous learner's draft after another learner signs in", async () => {
  await credentialStore.write(tokens);
  let value = profile;
  let active = true;
  server.use(
    http.get("*/v1/me", () =>
      active
        ? HttpResponse.json(value)
        : HttpResponse.json({}, { status: 401 }),
    ),
    http.post("*/v1/auth/refresh", () =>
      HttpResponse.json({}, { status: 401 }),
    ),
  );
  render(
    <RouteGuard>
      <label>
        Draft
        <input defaultValue="" />
      </label>
    </RouteGuard>,
  );
  const old = await screen.findByLabelText("Draft");
  fireEvent.change(old, { target: { value: "private old learner draft" } });
  active = false;
  await session.load();
  await screen.findByRole("button", { name: "Sign in again" });
  active = true;
  value = { ...profile, id: "8c940797-a555-421e-8dad-b4df96b02213" };
  await session.accept({ ...tokens, access_token: "another-learner" });
  await waitFor(() => expect(screen.getByLabelText("Draft")).not.toBe(old));
  expect(screen.getByLabelText("Draft")).toHaveValue("");
});

it("ignores an authenticated read that completes after logout", async () => {
  await credentialStore.write(tokens);
  let release!: () => void;
  const held = new Promise<void>((resolve) => {
    release = resolve;
  });
  let started = false;
  server.use(
    http.get("*/v1/me", async () => {
      started = true;
      await held;
      return HttpResponse.json(profile);
    }),
    http.post(
      "*/v1/auth/logout",
      () => new HttpResponse(null, { status: 204 }),
    ),
  );
  const pending = session.load();
  await waitFor(() => expect(started).toBe(true));
  await session.logout();
  release();
  expect(await pending).toBeNull();
  expect(session.current()).toBeNull();
  expect(await credentialStore.read()).toBeNull();
});
