import { appVersion, clientPlatform } from "../../lib/client-build";
import { isTauri } from "@tauri-apps/api/core";
import { configureAnalytics, setAnalyticsConsent } from "../../lib/analytics";
import { secureStore, type Tokens } from "./secure-store";
import { clearLessonCache } from "../player/cache";
import { clearLeagueCache } from "../gamification/league-cache";
import { clearSyncQueue } from "../player/sync";
import {
  clearOfflineIdentity,
  readOfflineIdentity,
  rememberOfflineIdentity,
} from "./offline-identity";
export const credentialStore = secureStore;
export type Learner = {
  id: string;
  display_name: string;
  locale: string;
  time_zone: string;
  background: string;
  analytics_opt_out: boolean;
};
const base = import.meta.env.VITE_API_URL ?? "http://localhost:8080";
/**
 * Browser sessions (openspec persist-web-session): the refresh credential lives in an
 * HttpOnly cookie the server sets and reads; script holds only the in-memory access token.
 * Cookie mode needs the API on the page's own origin (the deployed layout: Caddy serves
 * `/` and `/v1`), because credentialed cross-origin CORS is deliberately not enabled. A
 * cross-origin dev build and the Tauri app keep the body/secure-store flow.
 */
export function cookieMode(): boolean {
  if (isTauri()) return false;
  const local =
    location.hostname === "localhost" ||
    location.hostname === "127.0.0.1" ||
    location.hostname === "[::1]";
  return (
    (location.protocol === "https:" || local) &&
    (base === "" || base === location.origin)
  );
}
export const clientHeader = "X-Duolify-Client";
export function safeReturnTo(raw: string | null | undefined): string {
  if (!raw || /[\\\s%?#]/.test(raw)) return "/profile";
  return /^(\/|\/profile|\/profile\/setup|\/courses|\/courses\/create|\/credits|\/more|\/friends|\/leagues|\/notes|\/bookmarks|\/settings|\/settings\/sessions|\/streak|\/friends\/invite\/[A-Za-z0-9_-]{43}|\/course\/[a-zA-Z0-9_-]+|\/c\/[A-Za-z0-9_-]{20,64}|\/lesson\/[a-zA-Z0-9_-]+(?:\/test)?)$/.test(
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
  requestHeaders: Record<string, string> = {},
): Promise<T> {
  const cookieMutation =
    cookieMode() &&
    ([
      "/v1/auth/refresh",
      "/v1/auth/verify",
      "/v1/auth/logout",
      "/v1/auth/sso/complete",
      "/v1/me/deletion-request",
    ].includes(path) ||
      (path.startsWith("/v1/auth/sessions") && method === "DELETE"));
  if (cookieMutation) {
    if (!navigator.locks)
      throw new AuthError(
        "This browser cannot safely restore sessions. Please update your browser.",
        503,
      );
    return navigator.locks.request("duolify-auth-cookie", () =>
      performAuthRequest<T>(path, body, access, method, requestHeaders),
    );
  }
  return performAuthRequest<T>(path, body, access, method, requestHeaders);
}
async function performAuthRequest<T>(
  path: string,
  body: unknown,
  access: string | undefined,
  method: string,
  requestHeaders: Record<string, string>,
): Promise<T> {
  const response = await fetch(`${base}${path}`, {
    method,
    headers: {
      ...requestHeaders,
      ...(cookieMode() ? { [clientHeader]: "web" } : {}),
      "Content-Type": "application/json",
      ...(access ? { Authorization: `Bearer ${access}` } : {}),
    },
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
  });
  if (!response.ok) {
    let message = "Sign-in is unavailable. Please retry.";
    let details: unknown;
    try {
      details = await response.json();
      message = (details as { error?: string }).error || message;
    } catch {
      /* No server body on offline/proxy errors. */
    }
    throw new AuthError(message, response.status, details);
  }
  return response.status === 204
    ? (undefined as T)
    : (response.json() as Promise<T>);
}
async function authBlob(path: string, access: string): Promise<Blob> {
  const response = await fetch(`${base}${path}`, {
    headers: { Authorization: `Bearer ${access}` },
  });
  if (!response.ok) throw new AuthError("Download failed.", response.status);
  return response.blob();
}
async function authForm<T>(
  path: string,
  form: FormData,
  access: string,
): Promise<T> {
  // No Content-Type: the browser sets the multipart boundary itself.
  const response = await fetch(`${base}${path}`, {
    method: "POST",
    headers: {
      ...(cookieMode() ? { [clientHeader]: "web" } : {}),
      Authorization: `Bearer ${access}`,
    },
    body: form,
  });
  if (!response.ok) {
    let details: unknown;
    try {
      details = await response.json();
    } catch {
      /* No server body on proxy errors. */
    }
    const error = new AuthError(
      (details as { error?: string } | undefined)?.error || "Upload failed.",
      response.status,
      details,
    );
    const retry = Number(response.headers.get("Retry-After"));
    if (Number.isFinite(retry) && retry > 0) error.retryAfter = retry;
    throw error;
  }
  return response.json() as Promise<T>;
}
export class AuthError extends Error {
  retryAfter?: number;
  constructor(
    message: string,
    public status: number,
    public details?: unknown,
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
let cookieChecked = false;
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
    platform: clientPlatform(isTauri(), import.meta.env.VITE_MOBILE_PLATFORM),
    appVersion,
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
  clearOfflineIdentity();
  clearLeagueCache();
  await clearLessonCache();
  clearSyncQueue();
  learner = null;
  expired = true;
  // Keep only the last verified analytics identity/consent for the recovery
  // selection. Explicit logout clears it; no text or credentials are retained.
  publish();
}
// Cookie mode sends no body: the server reads the cookie and ignores anything else.
async function refreshRequest(tokens: Tokens | null): Promise<Tokens> {
  const web = cookieMode();
  const rotated = await authRequest<Tokens>(
    "/v1/auth/refresh",
    web ? undefined : { refresh_token: tokens?.refresh_token },
    undefined,
    "POST",
  );
  return web ? { ...rotated, refresh_token: "" } : rotated;
}
// First load of a page with no in-memory credentials: ask the cookie for an access token.
async function restoreFromCookie(expected: number): Promise<Tokens | null> {
  if (!cookieMode() || cookieChecked) return null;
  try {
    const restored = await refreshRequest(null);
    cookieChecked = true;
    if (expected !== generation) return null;
    await writeCredentials(restored, expected);
    return restored;
  } catch (error) {
    if (error instanceof AuthError && error.status < 500) {
      cookieChecked = true;
      return null;
    }
    throw error;
  }
}
async function rotate(tokens: Tokens, expected: number): Promise<Tokens> {
  if (!refreshing) {
    const work = (async () => {
      // Another caller may already have replaced the access token by the time
      // this 401 arrives. Reuse that result instead of replaying its old secret.
      const latest = await credentialStore.read();
      if (expected !== generation) throw new AuthError("Session changed.", 401);
      if (
        latest &&
        (cookieMode()
          ? latest.access_token !== tokens.access_token
          : latest.refresh_token !== tokens.refresh_token)
      )
        return latest;
      const boundLearner = learner?.id;
      const rotated = await refreshRequest(tokens);
      if (boundLearner) {
        const refreshedLearner = await authRequest<Learner>(
          "/v1/me",
          undefined,
          rotated.access_token,
        );
        if (expected !== generation)
          throw new AuthError("Session changed.", 401);
        if (refreshedLearner.id !== boundLearner) {
          await writeCredentials(null, expected);
          if (expected !== generation)
            throw new AuthError("Session changed.", 401);
          session.clear();
          expired = true;
          publish();
          throw new AuthError("Session changed. Sign in again.", 401);
        }
      }
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
    clearOfflineIdentity();
    clearLeagueCache();
    void clearLessonCache().catch(() => {});
    clearSyncQueue();
    try {
      for (const key of Object.keys(sessionStorage))
        if (
          key.startsWith("credits:recovery:") ||
          key.startsWith("player:position:") ||
          key.startsWith("course-outline:") ||
          key.startsWith("tutor-draft:")
        )
          sessionStorage.removeItem(key);
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
  async accept(issued: Tokens) {
    const tokens: Tokens = cookieMode()
      ? { ...issued, refresh_token: "" }
      : issued;
    cookieChecked = true;
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
      if (!cookieMode())
        await authRequest(
          "/v1/auth/logout",
          { refresh_token: tokens.refresh_token },
          undefined,
          "POST",
        ).catch(() => {});
      throw error;
    }
    if (learner?.id !== value.id) {
      clearLeagueCache();
      await clearLessonCache();
    }
    await rememberOfflineIdentity(value, tokens).catch(() => {});
    if (expected !== generation) throw new AuthError("Session changed.", 401);
    learner = value;
    expired = false;
    identify(value);
    publish();
    return value;
  },
  async request<T>(
    path: string,
    method = "GET",
    body?: unknown,
    requestHeaders: Record<string, string> = {},
  ): Promise<T> {
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
        requestHeaders,
      );
      if (expected !== generation) throw new AuthError("Session changed.", 401);
      return value;
    } catch (error) {
      if (!(error instanceof AuthError) || error.status !== 401) throw error;
      try {
        const rotated = await rotate(tokens, expected);
        if (expected !== generation)
          throw new AuthError("Session changed.", 401);
        const value = await authRequest<T>(
          path,
          body,
          rotated.access_token,
          method,
          requestHeaders,
        );
        if (expected !== generation)
          throw new AuthError("Session changed.", 401);
        return value;
      } catch (refreshError) {
        if (refreshError instanceof AuthError && refreshError.status === 401)
          await expire(expected);
        throw refreshError;
      }
    }
  },
  /** Authenticated multipart upload (voice transcription); rotates the session once on 401. */
  async upload<T>(path: string, form: FormData): Promise<T> {
    const expected = generation;
    const tokens = await credentialStore.read();
    if (!tokens || expected !== generation)
      throw new AuthError("Sign in again.", 401);
    try {
      return await authForm<T>(path, form, tokens.access_token);
    } catch (error) {
      if (!(error instanceof AuthError) || error.status !== 401) throw error;
      const rotated = await rotate(tokens, expected);
      if (expected !== generation) throw new AuthError("Session changed.", 401);
      return authForm<T>(path, form, rotated.access_token);
    }
  },
  /** Authenticated binary read (narration audio); rotates the session once on 401. */
  async blob(path: string): Promise<Blob> {
    const expected = generation;
    const tokens = await credentialStore.read();
    if (!tokens || expected !== generation)
      throw new AuthError("Sign in again.", 401);
    try {
      return await authBlob(path, tokens.access_token);
    } catch (error) {
      if (!(error instanceof AuthError) || error.status !== 401) throw error;
      const rotated = await rotate(tokens, expected);
      if (expected !== generation) throw new AuthError("Session changed.", 401);
      return authBlob(path, rotated.access_token);
    }
  },
  load(): Promise<Learner | null> {
    if (loading) return loading;
    const expected = generation;
    const work = (async () => {
      let tokens = await credentialStore.read();
      if (expected !== generation) return null;
      if (!tokens) tokens = await restoreFromCookie(expected);
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
        const latest = await credentialStore.read();
        if (latest && expected === generation)
          await rememberOfflineIdentity(value, latest).catch(() => {});
        if (expected !== generation) return null;
        expired = false;
        identify(value);
        publish();
        return value;
      } catch (error) {
        if (error instanceof AuthError && error.status === 401) return null;
        if (error instanceof TypeError || !navigator.onLine) {
          const offline = await readOfflineIdentity(tokens);
          if (expected !== generation) return null;
          if (offline) {
            learner = offline;
            expired = false;
            identify(offline);
            publish();
            return offline;
          }
        }
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
        await authRequest(
          "/v1/auth/logout",
          cookieMode() ? undefined : { refresh_token: tokens.refresh_token },
          undefined,
          "POST",
        );
    }
    if (started !== generation) return;
    const expected = ++generation;
    await writeCredentials(null, expected);
    session.clear();
  },
};
