import { isTauri } from "@tauri-apps/api/core";

export type OAuthCallback = { code: string; state: string } | null;
export interface OAuthBridge {
  openSystemBrowser(url: string): Promise<void>;
  listen(callback: (value: OAuthCallback) => void): Promise<() => void>;
  cancel?: () => void;
}
export type GoogleOAuthBridge = OAuthBridge;
export type OAuthProviderName = "google" | "apple";

declare global {
  interface Window {
    __DUOLIFY_OAUTH_BRIDGE__?: OAuthBridge & { cancel?: () => void };
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

const providerHosts: Record<OAuthProviderName, string[]> = {
  google: ["accounts.google.com", "accounts.google.test"],
  apple: ["appleid.apple.com", "appleid.apple.test"],
};

async function nativeBridge(provider: OAuthProviderName): Promise<OAuthBridge> {
  const [{ openUrl }, { getCurrent, onOpenUrl }] = await Promise.all([
    import("@tauri-apps/plugin-opener"),
    import("@tauri-apps/plugin-deep-link"),
  ]);
  return {
    async openSystemBrowser(raw) {
      const url = new URL(raw);
      if (
        url.protocol !== "https:" ||
        !providerHosts[provider].includes(url.hostname)
      )
        throw new Error("OAuth sign-in returned an unsafe browser URL.");
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

export async function oauthBridge(
  provider: OAuthProviderName,
): Promise<OAuthBridge> {
  if (typeof window !== "undefined" && window.__DUOLIFY_OAUTH_BRIDGE__)
    return window.__DUOLIFY_OAUTH_BRIDGE__;
  if (isTauri()) return nativeBridge(provider);
  return {
    async openSystemBrowser(raw) {
      const url = new URL(raw);
      if (
        url.protocol !== "https:" ||
        !providerHosts[provider].includes(url.hostname)
      )
        throw new Error("OAuth sign-in returned an unsafe browser URL.");
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

export const googleOAuthBridge = (): Promise<OAuthBridge> =>
  oauthBridge("google");
export const appleOAuthBridge = (): Promise<OAuthBridge> =>
  oauthBridge("apple");

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
