import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi, type Mock } from "vitest";
import { server } from "../../test/setup";
import { http, HttpResponse } from "msw";
import { SignIn } from "./SignIn";
import type { SSOCallback } from "./sso";
import { credentialStore, session } from "./session";
import { configureAnalytics } from "../../lib/analytics";

const posthog = vi.hoisted(() => ({
  capture: vi.fn(),
  has_opted_out_capturing: vi.fn(() => false),
  identify: vi.fn(),
  init: vi.fn(),
}));
vi.mock("posthog-js/dist/module.no-external", () => ({ default: posthog }));

const id = "56d4e614-7665-4f14-b83e-8b5e9bed384a";
const state = "s".repeat(43);
afterEach(async () => {
  await credentialStore.write(null);
  session.clear();
  configureAnalytics(null);
  localStorage.clear();
  delete window.__DUOLIFY_SSO_BRIDGE__;
  vi.clearAllMocks();
});

const enabled = () =>
  http.get("*/v1/auth/sso/config", () =>
    HttpResponse.json({ enabled: true, publishable_key: "pk_test_fixture" }),
  );
const start = () =>
  http.post("*/v1/auth/sso/start", () => HttpResponse.json({ state }));

function bridge(extra: { cancel?: () => void } = {}) {
  const handle: {
    callback?: (value: SSOCallback) => void;
    openSystemBrowser: Mock<(url: string) => Promise<void>>;
  } = {
    openSystemBrowser: vi
      .fn<(url: string) => Promise<void>>()
      .mockResolvedValue(undefined),
  };
  window.__DUOLIFY_SSO_BRIDGE__ = {
    openSystemBrowser: handle.openSystemBrowser,
    listen: async (listener) => {
      handle.callback = listener;
      return () => {};
    },
    ...extra,
  };
  return handle;
}

it("hides SSO buttons while Clerk is not configured", async () => {
  render(<SignIn />);
  await screen.findByRole("button", { name: "Get started" });
  expect(
    screen.queryByRole("button", { name: "Continue with Google" }),
  ).toBeNull();
  expect(
    screen.queryByRole("button", { name: "Continue with Apple" }),
  ).toBeNull();
});

it("completes Clerk sign-in through the deep-link bridge without exposing the verifier", async () => {
  const handle = bridge();
  let startBody: Record<string, unknown> = {};
  server.use(
    enabled(),
    http.post("*/v1/auth/sso/start", async ({ request }) => {
      startBody = (await request.json()) as Record<string, unknown>;
      return HttpResponse.json({ state });
    }),
    http.post("*/v1/auth/sso/complete", async ({ request }) => {
      const body = (await request.json()) as Record<string, unknown>;
      expect(body).toMatchObject({
        state,
        code: "c".repeat(43),
        redirect_uri: "duolify://sso/callback",
      });
      expect(body.code_verifier).toMatch(/^[A-Za-z0-9_-]{43}$/);
      expect(startBody.code_challenge).not.toBe(body.code_verifier);
      return HttpResponse.json({
        access_token: "private-access",
        refresh_token: "private-refresh",
        expires_in: 900,
      });
    }),
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
  fireEvent.click(screen.getByLabelText("I am 16 or older"));
  fireEvent.click(
    await screen.findByRole("button", { name: "Continue with Google" }),
  );
  await waitFor(() => expect(handle.openSystemBrowser).toHaveBeenCalledOnce());
  expect(startBody).toMatchObject({
    redirect_uri: "duolify://sso/callback",
    age_confirmed: true,
  });
  const opened = new URL(handle.openSystemBrowser.mock.calls[0][0] as string);
  expect(opened.pathname).toBe("/sso-bridge");
  expect(opened.searchParams.get("strategy")).toBe("oauth_google");
  expect(opened.searchParams.get("state")).toBe(state);
  expect(opened.href).not.toContain("verifier");
  expect(posthog.capture).toHaveBeenCalledWith(
    "ui_control_activated",
    expect.objectContaining({ surface: "sign_in", control_id: "sign_in" }),
  );
  // A retained callback from an earlier attempt must not cancel this attempt.
  handle.callback?.({ state: "old".repeat(15), code: "c".repeat(43) });
  handle.callback?.({ state: "old".repeat(15), cancelled: true });
  handle.callback?.({ state, code: "c".repeat(43) });
  await waitFor(() => expect(done).toHaveBeenCalledWith("/"));
  expect(JSON.stringify(posthog.capture.mock.calls)).not.toMatch(
    /private-access|private-refresh|sssss/,
  );
});

it("rejects a deep link whose state does not match the started transaction", async () => {
  const handle = bridge();
  let completed = false;
  server.use(
    enabled(),
    start(),
    http.post("*/v1/auth/sso/complete", () => {
      completed = true;
      return HttpResponse.json({});
    }),
  );
  render(<SignIn />);
  fireEvent.click(screen.getByLabelText("I am 16 or older"));
  fireEvent.click(
    await screen.findByRole("button", { name: "Continue with Apple" }),
  );
  await screen.findByRole("button", { name: "Cancel sign-in" });
  handle.callback?.({ state: "x".repeat(43), code: "c".repeat(43) });
  expect(screen.getByRole("button", { name: "Cancel sign-in" })).toBeVisible();
  expect(completed).toBe(false);
});

it("cancels SSO with focus recovery and respects analytics opt-out", async () => {
  const handle = bridge({ cancel: () => handle.callback?.(null) });
  server.use(enabled(), start());
  render(<SignIn />);
  fireEvent.click(screen.getByLabelText("I am 16 or older"));
  const google = await screen.findByRole("button", {
    name: "Continue with Google",
  });
  fireEvent.click(google);
  fireEvent.click(
    await screen.findByRole("button", { name: "Cancel sign-in" }),
  );
  await waitFor(() => expect(google).toHaveFocus());
  expect(screen.getByRole("status")).toHaveTextContent("cancelled");
  expect(posthog.capture).toHaveBeenCalledWith(
    "ui_recovery_selected",
    expect.objectContaining({
      source: "client",
      error_code: "auth_cancelled",
      action: "retry",
    }),
  );
  posthog.capture.mockClear();
  const analytics = await import("../../lib/analytics");
  analytics.trackSSOStarted(id, true);
  analytics.trackSSOCancelled(id, true);
  expect(posthog.capture).not.toHaveBeenCalled();
});

it("reports the mobile platform for SSO events before learner identity loads", async () => {
  vi.stubEnv("VITE_MOBILE_PLATFORM", "ios");
  Object.defineProperty(window, "__TAURI_INTERNALS__", {
    value: {},
    configurable: true,
  });
  try {
    const analytics = await import("../../lib/analytics");
    analytics.trackSSOStarted(id, false);
    expect(posthog.capture).toHaveBeenCalledWith(
      expect.any(String),
      expect.objectContaining({ platform: "ios" }),
    );
  } finally {
    delete (window as unknown as Record<string, unknown>).__TAURI_INTERNALS__;
    vi.unstubAllEnvs();
  }
});
