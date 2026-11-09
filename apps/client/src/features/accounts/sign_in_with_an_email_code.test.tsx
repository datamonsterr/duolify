import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { http, HttpResponse } from "msw";
import { server } from "../../test/setup";
import { SignIn } from "./SignIn";
import { safeReturnTo, session, credentialStore } from "./session";
import { createSecureStore } from "./secure-store";
import { configureAnalytics } from "../../lib/analytics";
const posthog = vi.hoisted(() => ({
  capture: vi.fn(),
  has_opted_out_capturing: vi.fn(() => false),
  identify: vi.fn(),
  init: vi.fn(),
}));
vi.mock("posthog-js/dist/module.no-external", () => ({ default: posthog }));
const id = "56d4e614-7665-4f14-b83e-8b5e9bed384a";
afterEach(async () => {
  await credentialStore.write(null);
  session.clear();
  configureAnalytics(null);
  localStorage.clear();
  vi.clearAllMocks();
});

it("rejects unsafe and encoded redirect paths", () => {
  for (const path of [
    "https://evil.test",
    "//evil.test",
    "/\\evil",
    "/%2f%2fevil.test",
    "/sign-in?returnTo=//evil.test",
    "/profile\n",
    "/course/../..",
  ])
    expect(safeReturnTo(path)).toBe("/profile");
  expect(safeReturnTo("/lesson/sky-changes")).toBe("/lesson/sky-changes");
});
it("uses a native credential bridge and never falls back after a native failure", async () => {
  const invoke = vi
    .fn()
    .mockResolvedValueOnce(null)
    .mockResolvedValueOnce(undefined);
  const secure = createSecureStore(invoke);
  expect(await secure.read()).toBeNull();
  await secure.write({
    access_token: "access",
    refresh_token: "refresh",
    expires_in: 900,
  });
  expect(invoke).toHaveBeenNthCalledWith(1, "read_credentials");
  expect(invoke).toHaveBeenNthCalledWith(2, "write_credentials", {
    value: JSON.stringify({
      access_token: "access",
      refresh_token: "refresh",
      expires_in: 900,
    }),
  });
  invoke.mockRejectedValueOnce(new Error("keychain locked"));
  await expect(secure.read()).rejects.toThrow("keychain locked");
  expect(localStorage.length).toBe(0);
});
it("keeps drafts and emits only stable sign-in properties after verified UUID identity", async () => {
  let valid = false;
  server.use(
    http.post("*/v1/auth/code", () => HttpResponse.json({}, { status: 202 })),
    http.post("*/v1/auth/verify", () =>
      valid
        ? HttpResponse.json({
            access_token: "private-access",
            refresh_token: "private-refresh",
            expires_in: 900,
          })
        : HttpResponse.json(
            { error: "Code is invalid or expired. Request a new code." },
            { status: 401 },
          ),
    ),
    http.get("*/v1/me", () =>
      HttpResponse.json({
        id,
        display_name: "Learner",
        locale: "en",
        time_zone: "UTC",
        background: "",
        analytics_opt_out: false,
      }),
    ),
  );
  const done = vi.fn();
  render(<SignIn onSignedIn={done} />);
  fireEvent.click(screen.getByRole("button", { name: "Get started" }));
  expect(screen.getByRole("button", { name: "Send code" })).toBeDisabled();
  fireEvent.change(screen.getByLabelText("Email address"), {
    target: { value: "private@example.test" },
  });
  fireEvent.click(screen.getByLabelText("I am 16 or older"));
  fireEvent.click(screen.getByRole("button", { name: "Send code" }));
  const code = await screen.findByLabelText("One-time code");
  expect(code).toHaveFocus();
  fireEvent.change(code, { target: { value: "123456" } });
  fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "invalid or expired",
  );
  expect(code).toHaveValue("123456");
  valid = true;
  fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
  await waitFor(() => expect(done).toHaveBeenCalled());
  expect(posthog.capture).toHaveBeenCalledWith("ui_control_activated", {
    distinct_id: id,
    platform: "web",
    app_version: "0.1.0",
    source: "client",
    surface: "sign_in",
    control_id: "sign_in",
  });
  expect(posthog.capture).toHaveBeenCalledWith(
    "ui_screen_viewed",
    expect.objectContaining({ distinct_id: id, screen_id: "sign_in" }),
  );
  expect(JSON.stringify(posthog.capture.mock.calls)).not.toMatch(
    /private|123456|@/,
  );
  configureAnalytics({ learnerId: id, platform: "web", optedOut: true });
  posthog.capture.mockClear();
  const { trackSignIn } = await import("../../lib/analytics");
  trackSignIn();
  expect(posthog.capture).not.toHaveBeenCalled();
  configureAnalytics({ learnerId: id, platform: "web", optedOut: false });
  posthog.has_opted_out_capturing.mockReturnValueOnce(true);
  trackSignIn();
  expect(posthog.capture).not.toHaveBeenCalled();
});
it("rotates an expired access token before retrying the authenticated learner read", async () => {
  await credentialStore.write({
    access_token: "expired",
    refresh_token: "old",
    expires_in: 900,
  });
  let refreshes = 0;
  server.use(
    http.get("*/v1/me", ({ request }) =>
      request.headers.get("Authorization") === "Bearer fresh"
        ? HttpResponse.json({
            id,
            display_name: "Learner",
            analytics_opt_out: true,
          })
        : HttpResponse.json({}, { status: 401 }),
    ),
    http.post("*/v1/auth/refresh", async ({ request }) => {
      expect(await request.json()).toEqual({ refresh_token: "old" });
      refreshes++;
      return HttpResponse.json({
        access_token: "fresh",
        refresh_token: "rotated",
        expires_in: 900,
      });
    }),
  );
  await Promise.all([session.load(), session.load()]);
  expect(refreshes).toBe(1);
  expect((await credentialStore.read())?.refresh_token).toBe("rotated");
  expect(posthog.capture).not.toHaveBeenCalled();
});

it("clears revoked credentials and marks the expired-session recovery", async () => {
  await credentialStore.write({
    access_token: "expired",
    refresh_token: "revoked",
    expires_in: 900,
  });
  server.use(
    http.get("*/v1/me", () => HttpResponse.json({}, { status: 401 })),
    http.post("*/v1/auth/refresh", () =>
      HttpResponse.json({}, { status: 401 }),
    ),
  );
  expect(await session.load()).toBeNull();
  expect(await credentialStore.read()).toBeNull();
  expect(session.expired()).toBe(true);
  expect(posthog.capture).not.toHaveBeenCalled();
});
