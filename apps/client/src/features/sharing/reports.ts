import { session } from "../accounts/session";

export type ReportTargetType = "course" | "review" | "user";
export type ReportTarget = { type: ReportTargetType; id: string };
export type ReportResult = { status: "received"; merged: boolean };

/** The same closed list the API validates (`SHARING_REPORT_REASONS`). */
export const reportReasons = [
  { id: "unsafe", label: "Unsafe instructions" },
  { id: "inappropriate", label: "Inappropriate content" },
  { id: "spam", label: "Spam" },
  { id: "copyright", label: "Copyright" },
  { id: "misleading", label: "Misleading" },
  { id: "other", label: "Other" },
];
/** Unicode characters, like the API and Postgres count them. */
export const reportMaxChars = 500;

export type ReportFailure =
  "unavailable" | "rate_limited" | "self" | "sign_in" | "offline";
/** Status only, never server text. 404 is the one neutral refusal. */
export function reportFailure(error: unknown): ReportFailure {
  switch ((error as { status?: number }).status) {
    case 404:
      return "unavailable";
    case 429:
      return "rate_limited";
    case 403:
      return "self";
    case 401:
      return "sign_in";
    default:
      return "offline";
  }
}
export const reportMessages: Record<ReportFailure, string> = {
  unavailable: "This isn't available anymore, so it can't be reported.",
  rate_limited: "Too many reports today. Try again tomorrow.",
  self: "You can't report your own content.",
  sign_in: "Sign in to send a report.",
  offline: "Couldn't reach Duolify. Check your connection and retry.",
};

/** The reporter always comes from the session; the server never echoes it. */
export const sendReport = (
  target: ReportTarget,
  reason: string,
  text: string,
) =>
  session.request<ReportResult>("/v1/marketplace/reports", "POST", {
    target_type: target.type,
    target_id: target.id,
    reason,
    text,
  });
