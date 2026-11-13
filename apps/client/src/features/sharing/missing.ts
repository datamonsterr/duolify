import { session } from "../accounts/session";

export type MissingState = "complete" | "locked" | "ready" | "generating";
export type MissingStatus = {
  course_id: string;
  version: number;
  total: number;
  ready: number;
  missing: number;
  state: MissingState;
  next_lesson_id?: string;
};
export type MissingQuote = {
  quote_id: string;
  lesson_id: string;
  pricing_version: string;
  content_credits: number;
  audio_credits: number;
  total_credits: number;
  balance: number;
  sufficient: boolean;
  expires_at?: string;
};
export type MissingFailure =
  | "insufficient_credits"
  | "stale_quote"
  | "locked"
  | "in_progress"
  | "unavailable"
  | "offline"
  | "failed";

const root = (course: string) =>
  `/v1/courses/${encodeURIComponent(course)}/missing-lessons`;

export const loadMissing = (course: string) =>
  session.request<MissingStatus>(root(course));
export const requestQuote = (course: string) =>
  session.request<MissingQuote>(`${root(course)}/quote`, "POST", {});
/** Consent is always explicit: the caller only gets here after the checkbox. */
export const generateNext = (course: string, quote: MissingQuote) =>
  session.request<{ generation_id: string; state: string }>(
    `${root(course)}/generate`,
    "POST",
    { lesson_id: quote.lesson_id, quote_id: quote.quote_id, consent: true },
  );
export const generationState = (id: string) =>
  session.request<{ state: string }>(
    `/v1/lesson-generations/${encodeURIComponent(id)}`,
  );

/** Status and the API's closed error codes only; server text is never shown. */
export function missingFailure(error: unknown): MissingFailure {
  const { status, message } = error as { status?: number; message?: string };
  if (status === 402) return "insufficient_credits";
  if (status === 409) {
    if (message === "quote_stale") return "stale_quote";
    if (message === "previous_lesson_not_passed") return "locked";
    return "in_progress";
  }
  if (status === 404) return "unavailable";
  if (status !== undefined && status >= 500) return "failed";
  return "offline";
}

export const missingMessages: Record<MissingFailure, string> = {
  insufficient_credits: "You don't have enough credits for this lesson.",
  stale_quote: "This price changed or expired. Review the new quote.",
  locked: "Pass the previous lesson test to unlock the next lesson.",
  in_progress: "This lesson is already being prepared.",
  unavailable: "This course isn't available.",
  offline: "Couldn't reach Duolify. Check your connection and retry.",
  failed: "Something went wrong. Try again in a moment.",
};
export const releasedMessage =
  "Your reserved credits were released. Try again — you'll see a new estimate.";
