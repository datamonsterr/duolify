import { isTauri } from "@tauri-apps/api/core";

export type OAuthCallback = { code: string; state: string } | null;
export interface GoogleOAuthBridge {
  openSystemBrowser(url: string): Promise<void>;
  listen(callback: (value: OAuthCallback) => void): Promise<() => void>;
  cancel?: () => void;
}

declare global {
  interface Window {
    __DUOLIFY_OAUTH_BRIDGE__?: GoogleOAuthBridge & { cancel?: () => void };
  }
}

function parseCallback(raw: string): OAuthCallback | undefined {
  let url: URL;
  try {
    url = new URL(raw);
  } catch {
    return undefined;
  }
  if (
    url.protocol !== "duolify:" ||
    url.hostname !== "oauth" ||
    url.pathname !== "/callback"
  )
    return undefined;
  if (url.searchParams.get("error") === "access_denied") return null;
  const code = url.searchParams.get("code");
  const state = url.searchParams.get("state");
  return code && state ? { code, state } : undefined;
}

async function nativeBridge(): Promise<GoogleOAuthBridge> {
  const [{ openUrl }, { getCurrent, onOpenUrl }] = await Promise.all([
    import("@tauri-apps/plugin-opener"),
    import("@tauri-apps/plugin-deep-link"),
  ]);
  return {
    async openSystemBrowser(raw) {
      const url = new URL(raw);
      if (
        url.protocol !== "https:" ||
        !["accounts.google.com", "accounts.google.test"].includes(url.hostname)
      )
        throw new Error("Google sign-in returned an unsafe browser URL.");
      await openUrl(url.href);
    },
    async listen(callback) {
      const current = await getCurrent();
      for (const raw of current ?? []) {
        const result = parseCallback(raw);
        if (result !== undefined) callback(result);
      }
      return onOpenUrl((urls) => {
        for (const raw of urls) {
          const result = parseCallback(raw);
          if (result !== undefined) callback(result);
        }
      });
    },
  };
}

export async function googleOAuthBridge(): Promise<GoogleOAuthBridge> {
  if (typeof window !== "undefined" && window.__DUOLIFY_OAUTH_BRIDGE__)
    return window.__DUOLIFY_OAUTH_BRIDGE__;
  if (isTauri()) return nativeBridge();
  return {
    async openSystemBrowser(raw) {
      const url = new URL(raw);
      if (
        url.protocol !== "https:" ||
        !["accounts.google.com", "accounts.google.test"].includes(url.hostname)
      )
        throw new Error("Google sign-in returned an unsafe browser URL.");
      window.open(url.href, "_blank", "noopener,noreferrer");
    },
    async listen(callback) {
      const onCallback = (event: Event) =>
        callback((event as CustomEvent<OAuthCallback>).detail);
      window.addEventListener("duolify-oauth-callback", onCallback);
      return () =>
        window.removeEventListener("duolify-oauth-callback", onCallback);
    },
  };
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
