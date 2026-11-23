// Mirrors the API limits a script validator may not exceed (presentation validation only).
// Source of truth: services/api. Keep in step when an API limit changes.
export const apiLimits: Record<
  string,
  { max?: number; min?: number; maxItems?: number }
> = {
  "profile.text": { max: 2000 },
  "profile.daily_minutes": { min: 1, max: 1440 },
  "profile.daily_xp": { min: 1, max: 10000 },
  "brief.text": { max: 500 },
  "steer.text": { max: 500 },
  "review.text": { max: 2000 },
  "report.text": { max: 1000 },
  "auth.code": { max: 6 },
  "draft.bytes": { max: 16384 },
};
export const maxDraftBytes = 16384;
