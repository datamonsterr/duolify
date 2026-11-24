import { Suspense, lazy, useEffect, useRef, useState } from "react";
import { Button } from "../design/Button";
import { Badge } from "../design/Badge";
import {
  trackAuthScreen,
  trackSSOCancelled,
  trackSSOStarted,
  trackSignIn,
} from "../../lib/analytics";
import {
  AuthError,
  analyticsOptOut,
  authRequest,
  safeReturnTo,
  session,
  setAnalyticsOptOut,
} from "./session";
import { isTurnstileFailure, useTurnstileSiteKey } from "./turnstile-config";
import { useTurnstileCheck } from "./turnstile-check";
import type { Tokens } from "./secure-store";
import {
  SSO_REDIRECT_URI,
  createPKCEVerifier,
  loadSSOConfig,
  pkceChallenge,
  rememberWebPending,
  ssoBridge,
  ssoBridgeUrl,
  type SSOCallback,
  type SSOConfig,
  type SSOStrategy,
} from "./sso";
import { isTauri } from "@tauri-apps/api/core";
import "./accounts.css";
import { SharedDemo } from "../courses/SharedDemo";
import { Brand } from "../design/brand/Brand";
const GuidedSignIn = lazy(() =>
  import("./GuidedSignIn").then((m) => ({ default: m.GuidedSignIn })),
);
import { guidedEnabled } from "../guided-chat/flags";

export function SignIn({
  onSignedIn,
  recovery = false,
}: {
  onSignedIn?: (path: string) => void;
  recovery?: boolean;
}) {
  const guided = guidedEnabled("signin");
  const [demo, setDemo] = useState(false);
  const demoTrigger = useRef<HTMLButtonElement>(null);
  const [step, setStep] = useState<"welcome" | "email" | "code">(
    recovery || new URLSearchParams(location.search).get("returning") === "true"
      ? "email"
      : "welcome",
  );
  const { siteKey, reload } = useTurnstileSiteKey(step !== "welcome");
  const emailCheck = useTurnstileCheck(
    siteKey,
    "email_code",
    !guided && step !== "welcome",
    step === "code",
    reload,
  );
  const signupCheck = useTurnstileCheck(
    siteKey,
    "sign_up",
    !guided && step === "code",
    false,
    reload,
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
  const [sso, setSSO] = useState<SSOConfig>({ enabled: false });
  const [info, setInfo] = useState<
    "Privacy" | "Terms" | "Analytics preference" | null
  >(null);
  const field = useRef<HTMLInputElement>(null);
  const actions = useRef<HTMLDivElement>(null);
  const oauthCleanup = useRef<(() => void) | null>(null);
  const oauthCancel = useRef<(() => void) | null>(null);
  const oauthState = useRef<{ state: string; verifier: string } | null>(null);
  const oauthCompleting = useRef(false);
  const oauthProvider = useRef<SSOStrategy>("oauth_google");
  const analyticsID = useRef(crypto.randomUUID());
  const destination = safeReturnTo(
    new URLSearchParams(location.search).get("returnTo") ?? "/",
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
    let live = true;
    void loadSSOConfig().then((config) => live && setSSO(config));
    return () => {
      live = false;
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
    const proof = emailCheck.consume();
    if (proof === null) return;
    setPending(true);
    setError("");
    try {
      await requestEmailCode(email, age, proof);
      if (step === "code") emailCheck.reset();
      setCode("");
      setStep("code");
      setNotice("Check your email. The code expires in 10 minutes.");
      field.current?.focus();
    } catch (cause) {
      if (cause instanceof AuthError && isTurnstileFailure(cause.details)) {
        emailCheck.fail();
        return;
      }
      emailCheck.reset();
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
    const proof = signupCheck.consume();
    if (proof === null) return;
    setPending(true);
    setError("");
    try {
      await verifyEmailCode(email, code, age, proof);
    } catch (cause) {
      if (cause instanceof AuthError && isTurnstileFailure(cause.details)) {
        signupCheck.fail();
        return;
      }
      signupCheck.reset();
      setError(
        cause instanceof Error
          ? cause.message
          : "Sign-in is unavailable. Please retry.",
      );
    } finally {
      setPending(false);
    }
  }
  async function requestEmailCode(
    address: string,
    confirmed: boolean,
    proof?: string,
  ) {
    await authRequest("/v1/auth/code", {
      email: address,
      age_confirmed: confirmed,
      turnstile_token: proof,
    });
  }
  async function verifyEmailCode(
    address: string,
    value: string,
    confirmed: boolean,
    proof?: string,
  ) {
    const tokens = await authRequest<Tokens>("/v1/auth/verify", {
      email: address,
      code: value,
      age_confirmed: confirmed,
      locale: navigator.language || "en",
      time_zone: Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC",
      analytics_opt_out: optOut,
      turnstile_token: proof,
    });
    setAnalyticsOptOut(optOut);
    await session.accept(tokens);
    trackAuthScreen("sign_in");
    trackSignIn();
    if (onSignedIn) onSignedIn(destination);
    else location.assign(destination);
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
    trackSSOCancelled(analyticsID.current, optOut);
    requestAnimationFrame(() =>
      document
        .querySelector<HTMLButtonElement>(`[data-sso-${oauthProvider.current}]`)
        ?.focus(),
    );
  }
  async function finishOAuth(value: SSOCallback) {
    if (!value) {
      finishOAuthCancel();
      return;
    }
    const current = oauthState.current;
    if (oauthCompleting.current) return;
    if (!current || value.state !== current.state) return;
    if ("cancelled" in value) {
      finishOAuthCancel();
      return;
    }
    oauthCompleting.current = true;
    setOAuthActive(false);
    setPending(true);
    setError("");
    try {
      const tokens = await authRequest<Tokens>("/v1/auth/sso/complete", {
        state: value.state,
        code: value.code,
        code_verifier: current.verifier,
        redirect_uri: SSO_REDIRECT_URI,
      });
      setAnalyticsOptOut(optOut);
      await session.accept(tokens);
      trackAuthScreen("sign_in");
      if (onSignedIn) onSignedIn(destination);
      else location.assign(destination);
    } catch (cause) {
      setError(
        cause instanceof Error
          ? cause.message
          : "Single sign-on is unavailable. Please retry.",
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
  async function signInWithSSO(strategy: SSOStrategy) {
    if (pending || !online || !age || !sso.enabled) return;
    oauthProvider.current = strategy;
    setPending(true);
    setError("");
    setNotice("");
    trackSSOStarted(analyticsID.current, optOut);
    try {
      const verifier = createPKCEVerifier();
      const transaction = await authRequest<{ state: string }>(
        "/v1/auth/sso/start",
        {
          redirect_uri: SSO_REDIRECT_URI,
          code_challenge: await pkceChallenge(verifier),
          age_confirmed: age,
          locale: navigator.language || "en",
          time_zone: Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC",
          analytics_opt_out: optOut,
        },
      );
      oauthState.current = { state: transaction.state, verifier };
      oauthCompleting.current = false;
      if (!isTauri() && !window.__DUOLIFY_SSO_BRIDGE__)
        rememberWebPending({
          state: transaction.state,
          verifier,
          returnTo: destination,
        });
      const bridge = await ssoBridge();
      const unlisten = await bridge.listen((value) => void finishOAuth(value));
      if (
        oauthState.current?.state !== transaction.state ||
        oauthCompleting.current
      ) {
        unlisten();
        return;
      }
      oauthCleanup.current = unlisten;
      oauthCancel.current = bridge.cancel ? () => bridge.cancel?.() : null;
      setOAuthActive(true);
      await bridge.openSystemBrowser(ssoBridgeUrl(transaction.state, strategy));
      setNotice(
        "Sign-in opens in your browser. Come back here when you're done.",
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
          : "Single sign-on is unavailable. Please retry.",
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
        <Brand href="/sign-in" />
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
                {sso.enabled && (
                  <>
                    <Button
                      data-sso-oauth_google
                      variant="secondary"
                      pending={pending && !oauthActive}
                      disabled={!age || !online || pending}
                      onClick={() => void signInWithSSO("oauth_google")}
                    >
                      Continue with Google
                    </Button>
                    <Button
                      data-sso-oauth_apple
                      variant="secondary"
                      pending={pending && !oauthActive}
                      disabled={!age || !online || pending}
                      onClick={() => void signInWithSSO("oauth_apple")}
                    >
                      Continue with Apple
                    </Button>
                  </>
                )}
                {oauthActive && (
                  <Button
                    variant="quiet"
                    onClick={() => {
                      if (oauthCancel.current) oauthCancel.current();
                      else finishOAuthCancel();
                    }}
                  >
                    Cancel sign-in
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
              {guided ? (
                <Suspense fallback={<p role="status" aria-busy="true" />}>
                  <GuidedSignIn
                    siteKey={siteKey}
                    reload={reload}
                    requestCode={requestEmailCode}
                    verifyCode={verifyEmailCode}
                  />
                </Suspense>
              ) : (
                <>
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
                        {emailCheck.widget}
                        <Button
                          type="submit"
                          pending={pending}
                          disabled={
                            !age ||
                            !email.trim() ||
                            !online ||
                            !emailCheck.ready
                          }
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
                            aria-describedby={
                              error ? "auth-error" : "auth-notice"
                            }
                          />
                        </label>
                        {signupCheck.widget}
                        <Button
                          type="submit"
                          pending={pending}
                          disabled={
                            code.length !== 6 || !online || !signupCheck.ready
                          }
                        >
                          Sign in
                        </Button>
                        <Button
                          variant="secondary"
                          disabled={pending || !online || !emailCheck.ready}
                          onClick={() => void send()}
                        >
                          Resend code
                        </Button>
                        {emailCheck.widget}
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
                </>
              )}
              <Button variant="quiet" disabled={pending} onClick={cancel}>
                Back to welcome
              </Button>
            </>
          )}
          {!online && !(guided && step !== "welcome") ? (
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
                      ? signInWithSSO(oauthProvider.current)
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
