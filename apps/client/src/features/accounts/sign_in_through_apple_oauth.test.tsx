import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { server } from "../../test/setup";
import { http, HttpResponse } from "msw";
import { SignIn } from "./SignIn";
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
afterEach(async () => {
  await credentialStore.write(null);
  session.clear();
  configureAnalytics(null);
  localStorage.clear();
  delete window.__DUOLIFY_OAUTH_BRIDGE__;
  vi.clearAllMocks();
});

it("completes Apple sign-in when Apple omits returning profile claims", async () => {
  let callback:
    ((value: { code: string; state: string } | null) => void) | undefined;
  const openSystemBrowser = vi.fn().mockResolvedValue(undefined);
  window.__DUOLIFY_OAUTH_BRIDGE__ = {
    openSystemBrowser,
    listen: async (listener) => {
      callback = listener;
      return () => {};
    },
  };
  server.use(
    http.post("*/v1/auth/apple/start", () =>
      HttpResponse.json({
        state: "state-fixture",
        nonce: "n".repeat(32),
        authorization_url:
          "https://appleid.apple.test/authorize?state=state-fixture",
      }),
    ),
    http.post("*/v1/auth/apple/complete", async ({ request }) => {
      const body = (await request.json()) as Record<string, unknown>;
      expect(body).toMatchObject({
        state: "state-fixture",
        nonce: "n".repeat(32),
        code: "private-code",
        redirect_uri: "duolify://oauth/callback",
        age_confirmed: true,
      });
      expect(body.code_verifier).toMatch(/^[A-Za-z0-9_-]{43}$/);
      return HttpResponse.json({
        access_token: "private-access",
        refresh_token: "private-refresh",
        expires_in: 900,
      });
    }),
    http.get("*/v1/me", () =>
      HttpResponse.json({
        id,
        display_name: "Existing Apple Learner",
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
  fireEvent.click(screen.getByRole("button", { name: "Continue with Apple" }));
  await waitFor(() => expect(openSystemBrowser).toHaveBeenCalledOnce());
  expect(posthog.capture).toHaveBeenCalledWith(
    "ui_control_activated",
    expect.objectContaining({
      distinct_id: expect.stringMatching(/^[0-9a-f-]{36}$/i),
      platform: "web",
      app_version: "0.1.0",
      source: "client",
      surface: "sign_in",
      control_id: "sign_in",
    }),
  );
  callback?.({ code: "private-code", state: "state-fixture" });
  await waitFor(() => expect(done).toHaveBeenCalledWith("/profile"));
  expect(JSON.stringify(posthog.capture.mock.calls)).not.toMatch(
    /private-code|private-access|private-refresh|state-fixture|nonce-fixture|Apple Learner/,
  );
});

it("cancels Apple sign-in with focus recovery and respects analytics opt-out", async () => {
  let callback: ((value: null) => void) | undefined;
  window.__DUOLIFY_OAUTH_BRIDGE__ = {
    openSystemBrowser: async () => {},
    listen: async (listener) => {
      callback = listener as (value: null) => void;
      return () => {};
    },
    cancel: () => callback?.(null),
  };
  server.use(
    http.post("*/v1/auth/apple/start", () =>
      HttpResponse.json({
        state: "state-fixture",
        nonce: "n".repeat(32),
        authorization_url:
          "https://appleid.apple.test/authorize?state=state-fixture",
      }),
    ),
  );
  render(<SignIn />);
  fireEvent.click(screen.getByLabelText("I am 16 or older"));
  const apple = screen.getByRole("button", { name: "Continue with Apple" });
  fireEvent.click(apple);
  await screen.findByRole("button", { name: "Cancel Apple sign-in" });
  fireEvent.click(screen.getByRole("button", { name: "Cancel Apple sign-in" }));
  await waitFor(() => expect(apple).toHaveFocus());
  expect(screen.getByRole("status")).toHaveTextContent("cancelled");
  expect(posthog.capture).toHaveBeenCalledWith(
    "ui_recovery_selected",
    expect.objectContaining({
      distinct_id: expect.stringMatching(/^[0-9a-f-]{36}$/i),
      platform: "web",
      app_version: "0.1.0",
      source: "client",
      error_code: "auth_cancelled",
      action: "retry",
    }),
  );
  posthog.capture.mockClear();
  const analytics = await import("../../lib/analytics");
  analytics.trackOAuthStarted(id, true);
  analytics.trackOAuthCancelled(id, true);
  expect(posthog.capture).not.toHaveBeenCalled();
});
