export type DailyFreeSummary = {
  available: number;
  granted: number;
  resets_at: string;
  timezone: string;
};

export type ActivityItem = {
  id: number;
  date: string;
  activity: string;
  detail?: string;
  source: string;
  amount_display: string;
  amount: number;
  kind: string;
};

export type ActivityPage = {
  entries: ActivityItem[];
  next_cursor: string;
};

export type WalletResponse = {
  plan?: {
    status?:
      | "active"
      | "cancel-scheduled"
      | "past-due"
      | "expired"
      | "refunded"
      | "disputed";
    pending_plan?: { name: string; effective_at: string };
    priority_generation?: boolean;
    billing_period?: "monthly" | "yearly";
    cancel_at_period_end?: boolean;
    revoked?: boolean;
    next_allowance_at?: string;
    plan_id: string;
    name: string;
    pricing_version: string;
    entitlement_version: string;
    monthly_credits: number;
    benefits: string[];
    concurrent_generation: number;
    paid_through: string;
    active: boolean;
    channel: string;
    management_url: string;
  };
  available: number;
  daily_free: DailyFreeSummary;
  purchased: number;
  subscription: number;
  reserved: number;
  reserved_note?: string;
  source_debt?: number;
  refund_channel?: string;
  cached?: boolean;
  unverified?: boolean;
  last_verified_at?: string;
  recent_activity: ActivityItem[];
};
