export type Offer = { sku: string; currency: string; amount_minor: number };
export type Pricing = {
  version: string;
  effective_from: string;
  currency: string;
  actions: Record<string, { credits: number }>;
  narration: { credits: number; characters: number; rounding: string };
  daily_free: {
    credits: number;
    eligible_actions: string[];
    expires: string;
    spend_first: boolean;
  };
  packs: (Offer & { name: string; credits: number })[];
  plans: {
    id: string;
    name: string;
    monthly: Offer;
    yearly: Offer;
    monthly_credits: number;
    concurrent_generation: number;
    benefits: string[];
  }[];
  sales_enabled: boolean;
  next_scheduled: { version: string; effective_from: string } | null;
};

export function isValidPricing(value: unknown): value is Pricing {
  if (!value || typeof value !== "object") return false;
  const config = value as Pricing;
  const actions = [
    "preview",
    "outline",
    "lesson_content",
    "narration",
    "tutor_short",
    "tutor_long",
    "quick_note",
    "cached_replay",
  ];
  return (
    typeof config.version === "string" &&
    config.version.length > 0 &&
    Number.isFinite(Date.parse(config.effective_from)) &&
    typeof config.currency === "string" &&
    actions.every(
      (action) =>
        Number.isSafeInteger(config.actions?.[action]?.credits) &&
        config.actions[action].credits >= 0,
    ) &&
    Number.isSafeInteger(config.daily_free?.credits) &&
    config.daily_free.credits >= 0 &&
    Number.isSafeInteger(config.narration?.credits) &&
    Number.isSafeInteger(config.narration?.characters) &&
    config.narration.characters > 0 &&
    Array.isArray(config.packs) &&
    config.packs.every(
      (pack) =>
        !!pack &&
        typeof pack === "object" &&
        typeof pack.sku === "string" &&
        typeof pack.currency === "string" &&
        Number.isSafeInteger(pack.amount_minor) &&
        pack.amount_minor >= 0 &&
        Number.isSafeInteger(pack.credits) &&
        pack.credits >= 0,
    ) &&
    Array.isArray(config.plans) &&
    config.plans.every(
      (plan) =>
        !!plan &&
        typeof plan === "object" &&
        typeof plan.id === "string" &&
        typeof plan.monthly?.sku === "string" &&
        typeof plan.monthly.currency === "string" &&
        typeof plan.yearly?.sku === "string" &&
        typeof plan.yearly.currency === "string" &&
        Number.isSafeInteger(plan.monthly_credits) &&
        plan.monthly_credits >= 0 &&
        Number.isSafeInteger(plan.monthly.amount_minor) &&
        plan.monthly.amount_minor >= 0 &&
        Number.isSafeInteger(plan.yearly.amount_minor) &&
        plan.yearly.amount_minor >= 0 &&
        Array.isArray(plan.benefits) &&
        plan.benefits.every((benefit) => typeof benefit === "string"),
    )
  );
}
