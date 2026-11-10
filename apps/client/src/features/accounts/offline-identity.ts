import type { Learner } from "./session";
import type { Tokens } from "./secure-store";
const key = "duolify.offline-identity";
async function binding(tokens: Tokens) {
  const raw = await crypto.subtle.digest(
    "SHA-256",
    new TextEncoder().encode(tokens.refresh_token),
  );
  return Array.from(new Uint8Array(raw), (b) =>
    b.toString(16).padStart(2, "0"),
  ).join("");
}
export function clearOfflineIdentity() {
  try {
    localStorage.removeItem(key);
  } catch {
    /* Disposable. */
  }
}
export async function rememberOfflineIdentity(value: Learner, tokens: Tokens) {
  const credential = await binding(tokens);
  try {
    localStorage.setItem(
      key,
      JSON.stringify({
        id: value.id,
        optOut: value.analytics_opt_out,
        credential,
      }),
    );
  } catch {
    /* Online sign-in remains available. */
  }
}
export async function readOfflineIdentity(
  tokens: Tokens,
): Promise<Learner | null> {
  try {
    const raw = JSON.parse(localStorage.getItem(key) ?? "null");
    if (
      !raw ||
      !/^[a-f0-9-]{36}$/.test(raw.id) ||
      typeof raw.optOut !== "boolean" ||
      raw.credential !== (await binding(tokens))
    )
      return null;
    return {
      id: raw.id,
      analytics_opt_out: raw.optOut,
      display_name: "",
      locale: "en",
      time_zone: "UTC",
      background: "",
    };
  } catch {
    return null;
  }
}
