export const reportReasons: { id: string; label: string }[] = [];
export type ReportFailure = "unavailable" | "rate_limited" | "self" | "offline";
export const reportFailure = (_error: unknown): ReportFailure => "offline";
