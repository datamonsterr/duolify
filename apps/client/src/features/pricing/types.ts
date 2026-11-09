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
