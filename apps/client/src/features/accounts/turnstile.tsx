import {
  useCallback,
  useEffect,
  useImperativeHandle,
  useRef,
  useState,
} from "react";
import { Button } from "../design/Button";
import { useUiPreferences } from "../design/localization/Preferences";
import type { TurnstileWidgetProps } from "./turnstile-check";
import "./turnstile.css";

const scriptURL =
  "https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit";
type Action = "email_code" | "sign_up" | "share_preview";

type TurnstileAPI = {
  render: (
    node: HTMLElement,
    options: {
      sitekey: string;
      action: Action;
      theme: "light" | "dark";
      language: "ar" | "en";
      size: "compact" | "flexible";
      appearance: "always" | "interaction-only";
      retry: "never";
      "refresh-expired": "manual";
      "refresh-timeout": "manual";
      "response-field": false;
      callback: (proof: string) => void;
      "error-callback": () => void;
      "expired-callback": () => void;
      "timeout-callback": () => void;
    },
  ) => string;
  remove: (id: string) => void;
};

let scriptReady: Promise<TurnstileAPI> | undefined;
const provider = () =>
  (window as Window & { turnstile?: TurnstileAPI }).turnstile;

function loadScript(): Promise<TurnstileAPI> {
  const existing = provider();
  if (existing) return Promise.resolve(existing);
  scriptReady ??= new Promise<TurnstileAPI>((resolve, reject) => {
    const script = document.createElement("script");
    script.src = scriptURL;
    script.async = true;
    const failed = () => {
      window.clearTimeout(timer);
      script.remove();
      reject(new Error("Turnstile script unavailable"));
    };
    const timer = window.setTimeout(failed, 8000);
    script.onload = () => {
      const api = provider();
      if (!api) return failed();
      window.clearTimeout(timer);
      resolve(api);
    };
    script.onerror = failed;
    document.head.append(script);
  }).catch((cause: unknown) => {
    scriptReady = undefined;
    throw cause;
  });
  return scriptReady;
}

type Status = "checking" | "ready" | "failed";

/** A widget instance holds its proof in memory and gives it to only one request. */
export function useTurnstile(
  siteKey: string | null,
  action: Action,
  active = true,
  interactionOnly = false,
  reloadConfig?: () => void,
  initiallyFailed = false,
) {
  const { preferences, t, theme } = useUiPreferences();
  const [container, setContainer] = useState<HTMLDivElement | null>(null);
  const [attempt, setAttempt] = useState(0);
  const proof = useRef<{ key: string; token: string } | null>(null);
  const retryButton = useRef<HTMLButtonElement>(null);
  const group = useRef<HTMLDivElement>(null);
  const language = preferences.locale === "ar" ? "ar" : "en";
  const key = `${action}:${siteKey}:${theme}:${language}:${interactionOnly}:${attempt}`;
  const [result, setResult] = useState<
    { key: string; status: Status } | undefined
  >(() => (initiallyFailed ? { key, status: "failed" } : undefined));
  const status = result?.key === key ? result.status : "checking";
  const fail = useCallback(() => {
    proof.current = null;
    setResult({ key, status: "failed" });
  }, [key]);
  const reset = useCallback(() => {
    proof.current = null;
    setAttempt((current) => current + 1);
  }, []);

  useEffect(() => {
    if (!siteKey || !container || !active) return;
    let live = true;
    let settled = false;
    let instance: { api: TurnstileAPI; id: string } | undefined;
    const failIfLive = () => {
      if (live) {
        settled = true;
        fail();
      }
    };
    void loadScript()
      .then((api) => {
        if (!live) return;
        const id = api.render(container, {
          sitekey: siteKey,
          action,
          theme,
          language,
          size:
            container.getBoundingClientRect().width < 300
              ? "compact"
              : "flexible",
          appearance: interactionOnly ? "interaction-only" : "always",
          retry: "never",
          "refresh-expired": "manual",
          "refresh-timeout": "manual",
          "response-field": false,
          callback: (token) => {
            if (!live || settled) return;
            if (!token) return failIfLive();
            settled = true;
            proof.current = { key, token };
            setResult({ key, status: "ready" });
          },
          "error-callback": failIfLive,
          "expired-callback": failIfLive,
          "timeout-callback": failIfLive,
        });
        instance = { api, id };
      })
      .catch(failIfLive);
    return () => {
      live = false;
      if (proof.current?.key === key) proof.current = null;
      if (instance) instance.api.remove(instance.id);
    };
  }, [
    action,
    active,
    container,
    fail,
    interactionOnly,
    key,
    language,
    siteKey,
    theme,
  ]);

  useEffect(() => {
    if (status === "failed") retryButton.current?.focus();
  }, [status]);

  const consume = useCallback(() => {
    if (status === "failed") return null;
    if (siteKey === "" || !active) return undefined;
    if (proof.current?.key !== key) return null;
    const token = proof.current.token;
    proof.current = null;
    return token;
  }, [active, key, siteKey, status]);

  const named = !interactionOnly || status === "failed";
  const widget =
    active && (siteKey || status === "failed") ? (
      <div
        className={named ? "turnstile-check" : undefined}
        ref={group}
        role={named ? "group" : undefined}
        aria-label={named ? t("turnstile.label") : undefined}
        tabIndex={named ? -1 : undefined}
      >
        <div ref={setContainer} hidden={status === "failed"} />
        {status === "checking" && !interactionOnly && (
          <p role="status">{t("turnstile.checking")}</p>
        )}
        {status === "failed" && (
          <>
            <p role="alert">{t("turnstile.failed")}</p>
            <Button
              ref={retryButton}
              variant="secondary"
              onClick={() => {
                group.current?.focus();
                if (!siteKey) reloadConfig?.();
                reset();
              }}
            >
              {t("turnstile.retry")}
            </Button>
          </>
        )}
      </div>
    ) : null;

  return {
    consume,
    fail,
    reset,
    ready:
      !active || status === "ready" || (siteKey === "" && status !== "failed"),
    widget,
  };
}

export default function TurnstileWidget({
  ref,
  siteKey,
  action,
  interactionOnly,
  reloadConfig,
  initiallyFailed,
}: TurnstileWidgetProps) {
  const { widget, consume, fail, reset, ready } = useTurnstile(
    siteKey,
    action,
    true,
    interactionOnly,
    reloadConfig,
    initiallyFailed,
  );
  useImperativeHandle(
    ref,
    () => ({ siteKey, interactionOnly, consume, fail, reset, ready }),
    [siteKey, interactionOnly, consume, fail, reset, ready],
  );
  return widget;
}
