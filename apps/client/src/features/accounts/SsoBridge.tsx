import { useEffect, useRef, useState } from "react";
import { Button } from "../design/Button";
import { Brand } from "../design/brand/Brand";
import { authRequest, safeReturnTo, session } from "./session";
import type { Tokens } from "./secure-store";
import {
  SSO_REDIRECT_URI,
  loadSSOConfig,
  takeWebPending,
  type SSOStrategy,
} from "./sso";
import "./accounts.css";

// The only page that loads Clerk. It runs in a normal browser (the system browser for
// the native apps) so Clerk's cookies, redirects and provider screens work as on any
// website. It hands the Clerk session token to the API, which verifies it, and then
// either finishes sign-in in this tab (web) or returns to the app through a deep link
// that carries only the one-time state (native).

const strategies: SSOStrategy[] = ["oauth_google", "oauth_apple"];
type Stage = "confirm" | "working" | "done" | "error";

function deepLink(state: string | null, denied = false) {
  const url = new URL(SSO_REDIRECT_URI);
  if (denied) url.searchParams.set("error", "access_denied");
  else if (state) url.searchParams.set("state", state);
  return url.href;
}

export function SsoBridge() {
  const params = new URLSearchParams(location.search);
  const state = params.get("state");
  const strategy = params.get("strategy") as SSOStrategy | null;
  const web = params.get("mode") === "web";
  const phase = params.get("phase");
  const valid = Boolean(
    state &&
    /^[A-Za-z0-9_-]{32,128}$/.test(state) &&
    strategy &&
    strategies.includes(strategy),
  );
  const [stage, setStage] = useState<Stage>(
    valid && (web || phase) ? "working" : valid ? "confirm" : "error",
  );
  const [message, setMessage] = useState(
    valid
      ? ""
      : "This sign-in link is not valid. Return to Duolify and try again.",
  );
  const started = useRef(false);

  async function run() {
    if (!valid || !state || !strategy) return;
    setStage("working");
    try {
      const config = await loadSSOConfig();
      if (!config.enabled || !config.publishableKey)
        throw new Error("Single sign-on is not available. Use an email code.");
      const { Clerk } = await import("@clerk/clerk-js");
      const clerk = new Clerk(config.publishableKey);
      await clerk.load();
      const here = (extra: Record<string, string>) => {
        const url = new URL(location.href);
        url.search = "";
        url.searchParams.set("state", state);
        url.searchParams.set("strategy", strategy);
        if (web) url.searchParams.set("mode", "web");
        for (const [key, value] of Object.entries(extra))
          url.searchParams.set(key, value);
        return url.href;
      };
      if (phase === "callback") {
        await clerk.handleRedirectCallback({
          signInForceRedirectUrl: here({ phase: "done" }),
          signUpForceRedirectUrl: here({ phase: "done" }),
        });
        return;
      }
      if (phase !== "done" || !clerk.session) {
        await clerk.client?.signIn.authenticateWithRedirect({
          strategy,
          redirectUrl: here({ phase: "callback" }),
          redirectUrlComplete: here({ phase: "done" }),
        });
        return;
      }
      const token = await clerk.session.getToken();
      if (!token)
        throw new Error("Sign-in could not be verified. Please retry.");
      await authRequest("/v1/auth/sso/handoff", {
        state,
        session_token: token,
      });
      // Clerk is only the identity check; Duolify keeps the session. Drop Clerk's.
      await clerk.signOut().catch(() => {});
      if (!web) {
        setStage("done");
        location.assign(deepLink(state));
        return;
      }
      const pending = takeWebPending();
      if (!pending || pending.state !== state)
        throw new Error("Sign-in could not be verified. Please retry.");
      const tokens = await authRequest<Tokens>("/v1/auth/sso/complete", {
        state,
        code_verifier: pending.verifier,
        redirect_uri: SSO_REDIRECT_URI,
      });
      await session.accept(tokens);
      location.assign(safeReturnTo(pending.returnTo));
    } catch (cause) {
      setStage("error");
      setMessage(
        cause instanceof Error
          ? cause.message
          : "Sign-in is unavailable. Please retry.",
      );
    }
  }

  useEffect(() => {
    if (started.current || stage !== "working") return;
    started.current = true;
    void run();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return (
    <div className="auth-page">
      <header className="auth-header">
        <Brand href="/sign-in" />
      </header>
      <main className="auth-main auth-steps">
        <div className="auth-copy">
          {stage === "confirm" && (
            <>
              <h1>Sign in to the Duolify app</h1>
              <p>
                Continue only if you just started sign-in from the Duolify app
                on your own device. If someone sent you this link, close this
                page.
              </p>
              <Button
                onClick={() => {
                  started.current = true;
                  void run();
                }}
              >
                Continue
              </Button>
              <a className="auth-legal-link" href={deepLink(null, true)}>
                Cancel and return to Duolify
              </a>
            </>
          )}
          {stage === "working" && (
            <>
              <h1>Signing you in</h1>
              <p role="status">One moment. Do not close this page.</p>
            </>
          )}
          {stage === "done" && (
            <>
              <h1>Return to Duolify</h1>
              <p role="status">
                You are signed in. If the app did not open, use the button
                below.
              </p>
              <a className="auth-legal-link" href={deepLink(state)}>
                Open Duolify
              </a>
            </>
          )}
          {stage === "error" && (
            <>
              <h1>Sign-in did not finish</h1>
              <p role="alert">{message}</p>
              <a
                className="auth-legal-link"
                href={web ? "/sign-in" : deepLink(null, true)}
              >
                {web ? "Back to sign in" : "Return to Duolify"}
              </a>
            </>
          )}
        </div>
      </main>
    </div>
  );
}
