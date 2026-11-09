import { secureStore } from "../accounts/secure-store";
export type ActionQuote = {
  id: string;
  pricing_version: string;
  action: string;
  currency: string;
  content_ceiling: number;
  audio_ceiling: number;
  daily_free_eligible: boolean;
  expires_at: string;
  accepted: boolean;
  expired?: boolean;
  bounds?: Record<string, number>;
  input?: { model: string; voice?: string };
  rate?: { model: string };
};
const base = import.meta.env.VITE_API_URL || "http://localhost:8080";
async function request<T>(path: string, body: unknown): Promise<T> {
  const tokens = await secureStore.read();
  const response = await fetch(base + path, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${tokens?.access_token ?? ""}`,
    },
    body: JSON.stringify(body),
  });
  if (!response.ok)
    throw new Error("Quote unavailable; renew quote and review terms.");
  return response.json() as Promise<T>;
}
export const quoteAPI = {
  get: (id: string) => {
    return (async () => {
      const tokens = await secureStore.read();
      const response = await fetch(
        `${base}/v1/quotes/${encodeURIComponent(id)}`,
        { headers: { Authorization: `Bearer ${tokens?.access_token ?? ""}` } },
      );
      if (!response.ok)
        throw new Error("Quote unavailable; renew quote and review terms.");
      return response.json() as Promise<ActionQuote>;
    })();
  },
  create: (action: string, narrationCharacters?: number) =>
    request<ActionQuote>("/v1/quotes", {
      action,
      narration_characters: narrationCharacters,
    }),
  accept: (id: string) =>
    request<{ accepted: boolean }>(
      `/v1/quotes/${encodeURIComponent(id)}/accept`,
      {},
    ),
};
