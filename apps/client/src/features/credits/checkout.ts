import { session } from "../accounts/session";
export type CheckoutInput = {
  request_id: string;
  sku: string;
  pricing_version: string;
  platform: "web" | "desktop" | "ios" | "android";
  return_to: "/credits";
  quote_id?: string;
};
export type CheckoutOrder = {
  id: string;
  state: "pending" | "paid" | "failed" | "expired";
  illustrative: boolean;
  sku: string;
  pricing_version: string;
  amount_minor: number;
  currency: string;
  credits: number;
  billing_period: string;
  return_to: string;
  quote_id: string;
};
export const checkoutAPI = {
  create: (input: CheckoutInput) =>
    session.request<CheckoutOrder>("/v1/credits/checkouts", "POST", input),
  get: (id: string) =>
    session.request<CheckoutOrder>(
      `/v1/credits/checkouts/${encodeURIComponent(id)}`,
    ),
};

// Only disposable identifiers, scoped to the authenticated account. Private work
// remains in its owning component; payment parameters are never read.
export function recoveryID(kind: "order" | "quote", value?: string | null) {
  const key = `credits:recovery:${session.current()?.id ?? "anonymous"}:${kind}`;
  try {
    if (value === null) sessionStorage.removeItem(key);
    else if (value !== undefined) sessionStorage.setItem(key, value);
    return sessionStorage.getItem(key) ?? undefined;
  } catch {
    return undefined;
  }
}
