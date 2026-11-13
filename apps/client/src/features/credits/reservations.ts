import { secureStore } from "../accounts/secure-store";
export type Reservation = {
  id: string;
  pricing_version: string;
  state: string;
  content_spent: number;
  audio_spent: number;
};
const base = import.meta.env.VITE_API_URL || "http://localhost:8080";
async function post(path: string): Promise<Reservation> {
  const tokens = await secureStore.read();
  const response = await fetch(base + path, {
    method: "POST",
    headers: { Authorization: `Bearer ${tokens?.access_token ?? ""}` },
  });
  if (!response.ok) {
    const body = (await response.json()) as {
      error: string;
      next_grant_at?: string;
    };
    if (body.error === "insufficient_credits") {
      throw new Error(
        `Not enough eligible credits. Next daily grant: ${body.next_grant_at ? new Date(body.next_grant_at).toLocaleString() : "check your wallet"}. View credit options in your wallet.`,
      );
    }
    if (body.error === "concurrency_limit" || body.error === "spend_cap") {
      throw new Error(
        "Action limit reached. Wait for the current action or try again after the daily reset.",
      );
    }
    throw new Error(
      "Reservation unavailable. Renew the quote and review current terms.",
    );
  }
  return response.json() as Promise<Reservation>;
}
export const reservationAPI = {
  reserve: (id: string) => post(`/v1/quotes/${encodeURIComponent(id)}/reserve`),
  release: (id: string) =>
    post(`/v1/credits/reservations/${encodeURIComponent(id)}/release`),
};
