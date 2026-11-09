import { isTauri } from "@tauri-apps/api/core";
import { configureAnalytics, setAnalyticsConsent } from "../../lib/analytics";
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
  return /^(\/|\/profile|\/profile\/setup|\/courses|\/courses\/create|\/credits|\/more|\/friends|\/leagues|\/notes|\/bookmarks|\/settings|\/settings\/sessions|\/streak|\/course\/[a-zA-Z0-9_-]+|\/lesson\/[a-zA-Z0-9_-]+(?:\/test)?)$/.test(
    raw,
  )
    ? raw
    : "/profile";
}
export async function authRequest<T>(
  path: string,
  body?: unknown,
  access?: string,
  method = body === undefined ? "GET" : "POST",
): Promise<T> {
  const response = await fetch(`${base}${path}`, {
    method,
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
  setAnalyticsConsent(!value);
}
let learner: Learner | null = null;
let expired = false;
let revision = 0;
let generation = 0;
let loading: Promise<Learner | null> | null = null;
let refreshing: Promise<Tokens> | null = null;
let writing: Promise<void> = Promise.resolve();
const listeners = new Set<() => void>();
function publish() {
  revision++;
  listeners.forEach((listener) => listener());
}
function identify(value: Learner) {
  configureAnalytics({
    learnerId: value.id,
    optedOut: value.analytics_opt_out || analyticsOptOut(),
    platform: isTauri() ? "desktop" : "web",
    appVersion: "0.1.0",
  });
}
// Serialize native keychain writes as well as browser writes. A late refresh
// cannot overtake credential deletion or a newer authenticated sign-in.
function writeCredentials(value: Tokens | null, expected: number) {
  const operation = writing
    .catch(() => {})
    .then(async () => {
      if (expected !== generation) throw new AuthError("Session changed.", 401);
      await credentialStore.write(value);
    });
  writing = operation;
  return operation;
}
async function expire(expected: number) {
  if (expected !== generation) return;
  await writeCredentials(null, expected);
  if (expected !== generation) return;
  generation++;
  learner = null;
  expired = true;
  // Keep only the last verified analytics identity/consent for the recovery
  // selection. Explicit logout clears it; no text or credentials are retained.
  publish();
}
async function rotate(tokens: Tokens, expected: number): Promise<Tokens> {
  if (!refreshing) {
    const work = (async () => {
      // Another caller may already have replaced the access token by the time
      // this 401 arrives. Reuse that result instead of replaying its old secret.
      const latest = await credentialStore.read();
      if (expected !== generation) throw new AuthError("Session changed.", 401);
      if (latest && latest.refresh_token !== tokens.refresh_token)
        return latest;
      const rotated = await authRequest<Tokens>("/v1/auth/refresh", {
        refresh_token: tokens.refresh_token,
      });
      await writeCredentials(rotated, expected);
      return rotated;
    })();
    refreshing = work;
    void work
      .finally(() => {
        if (refreshing === work) refreshing = null;
      })
      .catch(() => {});
  }
  return refreshing;
}
export const session = {
  current: () => learner,
  expired: () => expired,
  snapshot: () => revision,
  subscribe(listener: () => void) {
    listeners.add(listener);
    return () => {
      listeners.delete(listener);
    };
  },
  clear() {
    try {
      for (const key of Object.keys(sessionStorage))
        if (key.startsWith("credits:recovery:")) sessionStorage.removeItem(key);
    } catch {
      /* Disposable recovery metadata may be unavailable. */
    }
    generation++;
    learner = null;
    expired = false;
    loading = null;
    refreshing = null;
    configureAnalytics(null);
    publish();
  },
  async accept(tokens: Tokens) {
    const expected = ++generation;
    refreshing = null;
    let value: Learner;
    try {
      value = await authRequest<Learner>(
        "/v1/me",
        undefined,
        tokens.access_token,
      );
      await writeCredentials(tokens, expected);
      if (expected !== generation) throw new AuthError("Session changed.", 401);
    } catch (error) {
      await authRequest("/v1/auth/logout", {
        refresh_token: tokens.refresh_token,
      }).catch(() => {});
      throw error;
    }
    learner = value;
    expired = false;
    identify(value);
    publish();
    return value;
  },
  async request<T>(path: string, method = "GET", body?: unknown): Promise<T> {
    const expected = generation;
    const tokens = await credentialStore.read();
    if (!tokens || expected !== generation)
      throw new AuthError("Sign in again.", 401);
    try {
      const value = await authRequest<T>(
        path,
        body,
        tokens.access_token,
        method,
      );
      if (expected !== generation) throw new AuthError("Session changed.", 401);
      return value;
    } catch (error) {
      if (!(error instanceof AuthError) || error.status !== 401) throw error;
      try {
        const rotated = await rotate(tokens, expected);
        if (expected !== generation)
          throw new AuthError("Session changed.", 401);
        return await authRequest<T>(path, body, rotated.access_token, method);
      } catch (refreshError) {
        if (refreshError instanceof AuthError && refreshError.status === 401)
          await expire(expected);
        throw refreshError;
      }
    }
  },
  load(): Promise<Learner | null> {
    if (loading) return loading;
    const expected = generation;
    const work = (async () => {
      const tokens = await credentialStore.read();
      if (expected !== generation) return null;
      if (!tokens) {
        learner = null;
        publish();
        return null;
      }
      try {
        const value = await session.request<Learner>("/v1/me");
        if (expected !== generation) return null;
        learner = value;
        expired = false;
        identify(value);
        publish();
        return value;
      } catch (error) {
        if (error instanceof AuthError && error.status === 401) return null;
        throw error;
      }
    })();
    loading = work;
    void work
      .finally(() => {
        if (loading === work) loading = null;
      })
      .catch(() => {});
    return work;
  },
  async logout(allDevices = false) {
    const started = generation;
    if (allDevices) {
      await session.request("/v1/auth/sessions", "DELETE");
    } else {
      const tokens = await credentialStore.read();
      // Offline failure retains credentials so Retry can revoke the server family.
      if (tokens)
        await authRequest("/v1/auth/logout", {
          refresh_token: tokens.refresh_token,
        });
    }
    if (started !== generation) return;
    const expected = ++generation;
    await writeCredentials(null, expected);
    session.clear();
  },
};
