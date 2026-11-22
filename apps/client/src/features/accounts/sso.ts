import { isTauri } from "@tauri-apps/api/core";
import { authRequest } from "./session";

// Clerk SSO (ADR 0016). Clerk runs only on the web bridge page (/sso-bridge) in a
// normal browser. The native app opens that page in the system browser and gets back
// a deep link carrying the one-time `state` and a one-time `code` minted for the browser
// that signed in (RFC 8252). The app redeems both with its PKCE verifier, which never
// leaves the app: another device's app never sees the code, and an app that only
// intercepts the deep link lacks the verifier.

export type SSOStrategy = "oauth_google" | "oauth_apple";
export type SSOCallback = { state: string; code: string } | null;
export interface SSOBridge {
  openSystemBrowser(url: string): Promise<void>;
  listen(callback: (value: SSOCallback) => void): Promise<() => void>;
  cancel?: () => void;
}
export type SSOConfig = { enabled: boolean; publishableKey?: string };

export const SSO_REDIRECT_URI = "duolify://sso/callback";

declare global {
  interface Window {
    __DUOLIFY_SSO_BRIDGE__?: SSOBridge;
  }
}

/** Reads whether the API has Clerk configured. Any failure means SSO is off. */
export async function loadSSOConfig(): Promise<SSOConfig> {
  try {
    const config = await authRequest<{
      enabled?: boolean;
      publishable_key?: string;
    }>("/v1/auth/sso/config");
    return config.enabled && config.publishable_key
      ? { enabled: true, publishableKey: config.publishable_key }
      : { enabled: false };
  } catch {
    return { enabled: false };
  }
}

/** Origin that serves /sso-bridge: this web origin, or VITE_SSO_BRIDGE_URL in native builds. */
export function ssoBridgeOrigin(): string {
  const configured = import.meta.env.VITE_SSO_BRIDGE_URL as string | undefined;
  if (configured) return new URL(configured).origin;
  return isTauri() ? "https://duolify.online" : location.origin;
}

export function ssoBridgeUrl(state: string, strategy: SSOStrategy): string {
  const url = new URL("/sso-bridge", ssoBridgeOrigin());
  url.searchParams.set("state", state);
  url.searchParams.set("strategy", strategy);
  if (!isTauri()) url.searchParams.set("mode", "web");
  return url.href;
}

export function parseSSOCallback(raw: string): SSOCallback | undefined {
  let url: URL;
  try {
    url = new URL(raw);
  } catch {
    return undefined;
  }
  if (
    url.protocol !== "duolify:" ||
    url.hostname !== "sso" ||
    url.pathname !== "/callback"
  )
    return undefined;
  if (url.searchParams.get("error") === "access_denied") return null;
  const state = url.searchParams.get("state");
  const code = url.searchParams.get("code");
  return state && code ? { state, code } : undefined;
}

function assertBridgeUrl(raw: string) {
  const url = new URL(raw);
  const local =
    import.meta.env.DEV &&
    (url.hostname === "localhost" || url.hostname === "127.0.0.1");
  if (
    url.pathname !== "/sso-bridge" ||
    url.origin !== ssoBridgeOrigin() ||
    (url.protocol !== "https:" && !local)
  )
    throw new Error("Sign-in returned an unsafe browser URL.");
}

async function nativeBridge(): Promise<SSOBridge> {
  const [{ openUrl }, { getCurrent, onOpenUrl }] = await Promise.all([
    import("@tauri-apps/plugin-opener"),
    import("@tauri-apps/plugin-deep-link"),
  ]);
  return {
    async openSystemBrowser(raw) {
      assertBridgeUrl(raw);
      await openUrl(raw);
    },
    async listen(callback) {
      for (const raw of (await getCurrent()) ?? []) {
        const result = parseSSOCallback(raw);
        if (result !== undefined) callback(result);
      }
      return onOpenUrl((urls) => {
        for (const raw of urls) {
          const result = parseSSOCallback(raw);
          if (result !== undefined) callback(result);
        }
      });
    },
  };
}

export async function ssoBridge(): Promise<SSOBridge> {
  if (typeof window !== "undefined" && window.__DUOLIFY_SSO_BRIDGE__)
    return window.__DUOLIFY_SSO_BRIDGE__;
  if (isTauri()) return nativeBridge();
  // Web: the bridge page finishes the sign-in itself in this browser (mode=web), so
  // there is no deep link to listen for.
  return {
    async openSystemBrowser(raw) {
      assertBridgeUrl(raw);
      location.assign(raw);
    },
    async listen() {
      return () => {};
    },
  };
}

const WEB_PENDING_KEY = "duolify.sso.pending";
export type WebPending = { state: string; verifier: string; returnTo: string };

/** Keeps the verifier across the web redirect to Clerk and back. Tab-scoped, short-lived. */
export function rememberWebPending(value: WebPending) {
  try {
    sessionStorage.setItem(WEB_PENDING_KEY, JSON.stringify(value));
  } catch {
    /* Without sessionStorage the web flow cannot resume; sign-in reports an error. */
  }
}
export function takeWebPending(): WebPending | null {
  try {
    const raw = sessionStorage.getItem(WEB_PENDING_KEY);
    sessionStorage.removeItem(WEB_PENDING_KEY);
    return raw ? (JSON.parse(raw) as WebPending) : null;
  } catch {
    return null;
  }
}

export function createPKCEVerifier(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(32));
  return btoa(String.fromCharCode(...bytes))
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/g, "");
}

export async function pkceChallenge(verifier: string): Promise<string> {
  const digest = await crypto.subtle.digest(
    "SHA-256",
    new TextEncoder().encode(verifier),
  );
  return btoa(String.fromCharCode(...new Uint8Array(digest)))
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/g, "");
}
