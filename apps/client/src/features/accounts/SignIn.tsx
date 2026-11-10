import { useEffect, useRef, useState } from "react";
import { Button } from "../design/Button";
import { Badge } from "../design/Badge";
import {
  trackAuthScreen,
  trackOAuthCancelled,
  trackOAuthStarted,
  trackSignIn,
} from "../../lib/analytics";
import {
  analyticsOptOut,
  authRequest,
  safeReturnTo,
  session,
  setAnalyticsOptOut,
} from "./session";
import type { Tokens } from "./secure-store";
import {
  createPKCEVerifier,
  oauthBridge,
  pkceChallenge,
  type OAuthCallback,
  type OAuthProviderName,
} from "./google-oauth";
import "./accounts.css";
import { SharedDemo } from "../courses/SharedDemo";

export function SignIn({
  onSignedIn,
  recovery = false,
}: {
  onSignedIn?: (path: string) => void;
  recovery?: boolean;
}) {
  const [demo, setDemo] = useState(false);
  const demoTrigger = useRef<HTMLButtonElement>(null);
  const [step, setStep] = useState<"welcome" | "email" | "code">(
    recovery || new URLSearchParams(location.search).get("returning") === "true"
      ? "email"
      : "welcome",
  );
  const [returning, setReturning] = useState(
    recovery ||
      new URLSearchParams(location.search).get("returning") === "true",
  );
  const [email, setEmail] = useState("");
  const [code, setCode] = useState("");
  const [age, setAge] = useState(false);
  const [optOut, setOptOut] = useState(analyticsOptOut);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState(
    recovery || new URLSearchParams(location.search).get("reason") === "expired"
      ? "Session expired. Sign in again and we'll take you back to your lesson."
      : "",
  );
  const [online, setOnline] = useState(navigator.onLine);
  const [oauthActive, setOAuthActive] = useState(false);
  const [oauthProviderName, setOAuthProviderName] =
    useState<OAuthProviderName>("google");
  const [info, setInfo] = useState<
    "Privacy" | "Terms" | "Analytics preference" | null
  >(null);
  const field = useRef<HTMLInputElement>(null);
  const actions = useRef<HTMLDivElement>(null);
  const oauthCleanup = useRef<(() => void) | null>(null);
  const oauthCancel = useRef<(() => void) | null>(null);
  const oauthState = useRef<{
    state: string;
    nonce: string;
    verifier: string;
  } | null>(null);
  const oauthCompleting = useRef(false);
  const oauthProvider = useRef<OAuthProviderName>("google");
  const analyticsID = useRef(crypto.randomUUID());
  const destination = safeReturnTo(
    new URLSearchParams(location.search).get("returnTo"),
  );
  useEffect(() => {
    const update = () => setOnline(navigator.onLine);
    window.addEventListener("online", update);
    window.addEventListener("offline", update);
    return () => {
      window.removeEventListener("online", update);
      window.removeEventListener("offline", update);
    };
  }, []);
  useEffect(() => {
    field.current?.focus();
    if (step === "welcome")
      actions.current
        ?.querySelector<HTMLButtonElement>("[data-welcome-primary]")
        ?.focus();
    trackAuthScreen(step === "welcome" ? "welcome" : "sign_in");
  }, [step]);
  function open(existing: boolean) {
    setReturning(existing);
    setStep("email");
    setError("");
    setNotice("");
  }
  async function send() {
    setPending(true);
    setError("");
    try {
      await authRequest("/v1/auth/code", { email, age_confirmed: age });
      setCode("");
      setStep("code");
      setNotice("Check your email. The code expires in 10 minutes.");
      field.current?.focus();
    } catch (cause) {
      setError(
        cause instanceof Error
          ? cause.message
          : "Sign-in is unavailable. Please retry.",
      );
    } finally {
      setPending(false);
    }
  }
  async function verify() {
    setPending(true);
    setError("");
    try {
      const tokens = await authRequest<Tokens>("/v1/auth/verify", {
        email,
        code,
        age_confirmed: age,
        locale: navigator.language || "en",
        time_zone: Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC",
        analytics_opt_out: optOut,
      });
      setAnalyticsOptOut(optOut);
      await session.accept(tokens);
      trackAuthScreen("sign_in");
      trackSignIn();
      if (onSignedIn) onSignedIn(destination);
      else location.assign(destination);
    } catch (cause) {
      setError(
        cause instanceof Error
          ? cause.message
          : "Sign-in is unavailable. Please retry.",
      );
    } finally {
      setPending(false);
    }
  }
  function finishOAuthCancel() {
    if (!oauthState.current || oauthCompleting.current) return;
    oauthCleanup.current?.();
    oauthCleanup.current = null;
    oauthState.current = null;
    oauthCompleting.current = false;
    oauthCancel.current = null;
    setOAuthActive(false);
    setPending(false);
    setError("");
    setNotice(
      "Sign-in was cancelled. No account changes were made. Try again any time.",
    );
    trackOAuthCancelled(analyticsID.current, optOut);
    requestAnimationFrame(() =>
      document
        .querySelector<HTMLButtonElement>(
          `[data-${oauthProvider.current}-sign-in]`,
        )
        ?.focus(),
    );
  }
  async function finishOAuth(value: OAuthCallback) {
    if (!value) {
      finishOAuthCancel();
      return;
    }
    const current = oauthState.current;
    if (oauthCompleting.current) return;
    if (!current || value.state !== current.state) {
      setError("Sign-in could not be verified. Please retry.");
      setOAuthActive(false);
      setPending(false);
      oauthCleanup.current?.();
      oauthCleanup.current = null;
      oauthState.current = null;
      oauthCancel.current = null;
      return;
    }
    oauthCompleting.current = true;
    setOAuthActive(false);
    setPending(true);
    setError("");
    try {
      const tokens = await authRequest<Tokens>(
        `/v1/auth/${oauthProvider.current}/complete`,
        {
          state: value.state,
          nonce: current.nonce,
          code: value.code,
          code_verifier: current.verifier,
          redirect_uri: "duolify://oauth/callback",
          age_confirmed: age,
          locale: navigator.language || "en",
          time_zone: Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC",
          analytics_opt_out: optOut,
        },
      );
      setAnalyticsOptOut(optOut);
      await session.accept(tokens);
      trackAuthScreen("sign_in");
      if (onSignedIn) onSignedIn(destination);
      else location.assign(destination);
    } catch (cause) {
      setError(
        cause instanceof Error
          ? cause.message
          : `${oauthProvider.current === "apple" ? "Apple" : "Google"} sign-in is unavailable. Please retry.`,
      );
    } finally {
      oauthCleanup.current?.();
      oauthCleanup.current = null;
      oauthState.current = null;
      oauthCompleting.current = false;
      oauthCancel.current = null;
      setOAuthActive(false);
      setPending(false);
    }
  }
  async function signInWithOAuth(provider: OAuthProviderName) {
    if (pending || !online || !age) return;
    oauthProvider.current = provider;
    setOAuthProviderName(provider);
    setPending(true);
    setError("");
    setNotice("");
    trackOAuthStarted(analyticsID.current, optOut);
    try {
      const verifier = createPKCEVerifier();
      const transaction = await authRequest<{
        state: string;
        nonce: string;
        authorization_url: string;
      }>(`/v1/auth/${provider}/start`, {
        redirect_uri: "duolify://oauth/callback",
        code_challenge: await pkceChallenge(verifier),
      });
      oauthState.current = {
        state: transaction.state,
        nonce: transaction.nonce,
        verifier,
      };
      oauthCompleting.current = false;
      const bridge = await oauthBridge(provider);
      oauthCleanup.current = await bridge.listen(
        (value) => void finishOAuth(value),
      );
      oauthCancel.current = bridge.cancel ? () => bridge.cancel?.() : null;
      setOAuthActive(true);
      await bridge.openSystemBrowser(transaction.authorization_url);
      setNotice(
        `${provider === "apple" ? "Apple" : "Google"} sign-in opens in your browser. Come back here when you're done.`,
      );
      setPending(false);
    } catch (cause) {
      oauthCleanup.current?.();
      oauthCleanup.current = null;
      oauthState.current = null;
      setOAuthActive(false);
      setError(
        cause instanceof Error
          ? cause.message
          : `${provider === "apple" ? "Apple" : "Google"} sign-in is unavailable. Please retry.`,
      );
      setPending(false);
    }
  }
  function cancel() {
    setStep("welcome");
    setCode("");
    setError("");
    setNotice("Sign-in was cancelled. Try again any time.");
  }
  const content = (
    <div className="auth-page" hidden={demo} inert={demo}>
      <header className="auth-header">
        <a className="brand" href="/sign-in">
          <span className="brand-mark" aria-hidden="true">
            <span />
            <span />
          </span>
          duolify
        </a>
        {step === "welcome" && (
          <Button variant="secondary" onClick={() => open(true)}>
            I have an account
          </Button>
        )}
      </header>
      <main
        className={`auth-main ${step === "welcome" ? "auth-welcome" : "auth-steps"}`}
      >
        <div className="auth-copy">
          {step === "welcome" ? (
            <>
              <h1>Learn any topic, a little at a time.</h1>
              <p>
                Tell us what you're curious about. We'll chat for a minute, then
                build short lessons, quick tests and a streak that grows with
                you.
              </p>
              <div className="auth-chips">
                <Badge role="success">Pick a topic</Badge>
                <Badge role="info">Chat to set it up</Badge>
                <Badge role="streak">Learn daily</Badge>
              </div>
              <div className="auth-actions" ref={actions}>
                <label className="auth-check">
                  <input
                    type="checkbox"
                    checked={age}
                    onChange={(event) => setAge(event.target.checked)}
                  />
                  I am 16 or older
                </label>
                <Button
                  data-google-sign-in
                  variant="secondary"
                  pending={pending && !oauthActive}
                  disabled={!age || !online || pending}
                  onClick={() => void signInWithOAuth("google")}
                >
                  Continue with Google
                </Button>
                <Button
                  data-apple-sign-in
                  variant="secondary"
                  pending={pending && !oauthActive}
                  disabled={!age || !online || pending}
                  onClick={() => void signInWithOAuth("apple")}
                >
                  Continue with Apple
                </Button>
                {oauthActive && (
                  <Button
                    variant="quiet"
                    onClick={() => {
                      if (oauthCancel.current) oauthCancel.current();
                      else finishOAuthCancel();
                    }}
                  >
                    Cancel {oauthProviderName === "apple" ? "Apple" : "Google"}{" "}
                    sign-in
                  </Button>
                )}
                <Button data-welcome-primary onClick={() => open(false)}>
                  Get started
                </Button>
                <Button
                  ref={demoTrigger}
                  variant="secondary"
                  onClick={() => setDemo(true)}
                >
                  Explore shared example
                </Button>
                <Button variant="secondary" onClick={() => open(true)}>
                  I already have an account
                </Button>
              </div>
            </>
          ) : (
            <>
              <h1>
                {returning
                  ? "Welcome back"
                  : step === "email"
                    ? "Start your learning journey"
                    : "Check your email"}
              </h1>
              <p>
                {step === "email"
                  ? "Sign in with an email code. No password needed."
                  : `Enter the six-digit code sent to ${email}.`}
              </p>
              <form
                onSubmit={(event) => {
                  event.preventDefault();
                  if (!pending && online)
                    void (step === "email" ? send() : verify());
                }}
                aria-label="Email sign-in"
              >
                {step === "email" ? (
                  <>
                    <label>
                      Email address
                      <input
                        ref={field}
                        type="email"
                        autoComplete="email"
                        required
                        maxLength={254}
                        value={email}
                        onChange={(event) => setEmail(event.target.value)}
                      />
                    </label>
                    <label className="auth-check">
                      <input
                        type="checkbox"
                        checked={age}
                        onChange={(event) => setAge(event.target.checked)}
                      />
                      I am 16 or older
                    </label>
                    <Button
                      type="submit"
                      pending={pending}
                      disabled={!age || !email.trim() || !online}
                    >
                      Send code
                    </Button>
                  </>
                ) : (
                  <>
                    <label>
                      One-time code
                      <input
                        ref={field}
                        inputMode="numeric"
                        autoComplete="one-time-code"
                        pattern="[0-9]{6}"
                        required
                        maxLength={6}
                        value={code}
                        onChange={(event) =>
                          setCode(event.target.value.replace(/\D/g, ""))
                        }
                        aria-describedby={error ? "auth-error" : "auth-notice"}
                      />
                    </label>
                    <Button
                      type="submit"
                      pending={pending}
                      disabled={code.length !== 6 || !online}
                    >
                      Sign in
                    </Button>
                    <Button
                      variant="secondary"
                      disabled={pending || !online}
                      onClick={() => void send()}
                    >
                      Resend code
                    </Button>
                    <Button
                      variant="quiet"
                      disabled={pending}
                      onClick={() => {
                        setStep("email");
                        setCode("");
                        setError("");
                        setNotice("");
                      }}
                    >
                      Change email
                    </Button>
                  </>
                )}
              </form>
              <Button variant="quiet" disabled={pending} onClick={cancel}>
                Back to welcome
              </Button>
            </>
          )}
          {!online ? (
            <p role="status" className="auth-warning">
              Offline. Connect to sign in. Saved lessons open after sign-in.
            </p>
          ) : (
            notice && (
              <p role="status" id="auth-notice">
                {notice}
              </p>
            )
          )}
          {error && (
            <div role="alert" className="auth-error" id="auth-error">
              <p>{error}</p>
              <Button
                variant="secondary"
                disabled={pending || !online}
                onClick={() =>
                  void (step === "code"
                    ? verify()
                    : step === "welcome"
                      ? signInWithOAuth(oauthProvider.current)
                      : send())
                }
              >
                Retry sign-in
              </Button>
            </div>
          )}
          <footer className="auth-legal">
            {(["Privacy", "Terms", "Analytics preference"] as const).map(
              (item) => (
                <Button
                  key={item}
                  variant="quiet"
                  className="auth-legal-link"
                  onClick={() => setInfo(info === item ? null : item)}
                  aria-expanded={info === item}
                >
                  {item}
                </Button>
              ),
            )}
          </footer>
          {info && (
            <section className="auth-info" aria-label={info}>
              <h2>{info}</h2>
              {info === "Analytics preference" ? (
                <label className="auth-check">
                  <input
                    type="checkbox"
                    checked={!optOut}
                    onChange={(event) => {
                      setOptOut(!event.target.checked);
                      setAnalyticsOptOut(!event.target.checked);
                    }}
                  />
                  Share anonymous product analytics
                </label>
              ) : info === "Privacy" ? (
                <p>
                  We use your email to sign you in. Your codes and tokens are
                  never sent to analytics. Optional profile details help AI
                  tailor examples. You control your data and analytics
                  preference.
                </p>
              ) : (
                <p>
                  Duolify is for learners aged 16 and older. Email codes are for
                  your use only. Public launch terms are awaiting legal review.
                </p>
              )}
              <Button variant="quiet" onClick={() => setInfo(null)}>
                Close {info}
              </Button>
            </section>
          )}
        </div>
        {step === "welcome" && (
          <div className="auth-art" aria-hidden="true">
            <div className="auth-orbit" />
            <i />
            <i />
            <i />
            <i />
            <i />
          </div>
        )}
      </main>
    </div>
  );
  return (
    <>
      {demo && (
        <SharedDemo
          surface="welcome"
          onReturn={() => {
            setDemo(false);
            requestAnimationFrame(() => demoTrigger.current?.focus());
          }}
        />
      )}
      {content}
    </>
  );
}
