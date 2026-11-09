import { isTauri } from "@tauri-apps/api/core";
import { configureAnalytics } from "../../lib/analytics";
import { secureStore, type Tokens } from "./secure-store";
export const credentialStore = secureStore;
export type Learner = {
  id: string;
  display_name: string;
  locale: string;
  time_zone: string;
  background: string;
  analytics_opt_out: boolean;
};
const base = import.meta.env.VITE_API_URL || "http://localhost:8080";
export function safeReturnTo(raw: string | null | undefined): string {
  if (!raw || /[\\\s%?#]/.test(raw)) return "/profile";
  return /^(\/|\/profile|\/courses|\/credits|\/more|\/friends|\/leagues|\/notes|\/bookmarks|\/settings|\/streak|\/course\/[a-zA-Z0-9_-]+|\/lesson\/[a-zA-Z0-9_-]+(?:\/test)?)$/.test(
    raw,
  )
    ? raw
    : "/profile";
}
export async function authRequest<T>(
  path: string,
  body?: unknown,
  access?: string,
): Promise<T> {
  const response = await fetch(`${base}${path}`, {
    method: body === undefined ? "GET" : "POST",
    headers: {
      "Content-Type": "application/json",
      ...(access ? { Authorization: `Bearer ${access}` } : {}),
    },
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
  });
  if (!response.ok) {
    let message = "Sign-in is unavailable. Please retry.";
    try {
      message =
        ((await response.json()) as { error?: string }).error || message;
    } catch {
      /* No server body on offline/proxy errors. */
    }
    throw new AuthError(message, response.status);
  }
  return response.status === 204
    ? (undefined as T)
    : (response.json() as Promise<T>);
}
export class AuthError extends Error {
  constructor(
    message: string,
    public status: number,
  ) {
    super(message);
  }
}
export function analyticsOptOut(): boolean {
  try {
    return localStorage.getItem("duolify.analytics-opt-out") === "true";
  } catch {
    return false;
  }
}
export function setAnalyticsOptOut(value: boolean) {
  try {
    localStorage.setItem("duolify.analytics-opt-out", String(value));
  } catch {
    /* Retain the current UI choice. */
  }
  if (value) configureAnalytics(null);
}
let learner: Learner | null = null;
let expired = false;
let loading: Promise<Learner | null> | null = null;
function identify(value: Learner) {
  configureAnalytics({
    learnerId: value.id,
    optedOut: value.analytics_opt_out || analyticsOptOut(),
    platform: isTauri() ? "desktop" : "web",
    appVersion: "0.1.0",
  });
}
export const session = {
  current: () => learner,
  expired: () => expired,
  clear() {
    learner = null;
    expired = false;
    loading = null;
    configureAnalytics(null);
  },
  async accept(tokens: Tokens) {
    // Verify identity before exposing credentials or configuring analytics.
    let value: Learner;
    try {
      value = await authRequest<Learner>(
        "/v1/me",
        undefined,
        tokens.access_token,
      );
      await credentialStore.write(tokens);
    } catch (error) {
      await authRequest("/v1/auth/logout", {
        refresh_token: tokens.refresh_token,
      }).catch(() => {});
      throw error;
    }
    learner = value;
    expired = false;
    identify(value);
    return value;
  },
  load(): Promise<Learner | null> {
    if (loading) return loading;
    loading = (async () => {
      const tokens = await credentialStore.read();
      if (!tokens) {
        session.clear();
        return null;
      }
      let value: Learner;
      try {
        value = await authRequest<Learner>(
          "/v1/me",
          undefined,
          tokens.access_token,
        );
      } catch (error) {
        if (!(error instanceof AuthError) || error.status !== 401) throw error;
        let rotated: Tokens;
        try {
          rotated = await authRequest<Tokens>("/v1/auth/refresh", {
            refresh_token: tokens.refresh_token,
          });
        } catch (refreshError) {
          if (
            refreshError instanceof AuthError &&
            refreshError.status === 401
          ) {
            await credentialStore.write(null);
            session.clear();
            expired = true;
            return null;
          }
          throw refreshError;
        }
        return session.accept(rotated);
      }
      learner = value;
      identify(value);
      return value;
    })().finally(() => {
      loading = null;
    });
    return loading;
  },
  async logout() {
    const tokens = await credentialStore.read();
    // Keep credentials if revocation fails, so Retry can actually revoke the server session.
    if (tokens)
      await authRequest("/v1/auth/logout", {
        refresh_token: tokens.refresh_token,
      });
    await credentialStore.write(null);
    session.clear();
  },
};
