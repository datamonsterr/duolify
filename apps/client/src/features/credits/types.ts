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
  available: number;
  daily_free: DailyFreeSummary;
  purchased: number;
  subscription: number;
  reserved: number;
  reserved_note?: string;
  cached?: boolean;
  unverified?: boolean;
  last_verified_at?: string;
  recent_activity: ActivityItem[];
};
