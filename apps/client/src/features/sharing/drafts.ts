import { session } from "../accounts/session";

export type Verdict = "pass" | "needs_review" | "block" | "pending_screening";
export type Finding = {
  kind: "personal_data" | "secret" | "out_of_allowlist";
  location: string;
};
export type PublicationDraft = {
  id: string;
  course_id: string;
  number: number;
  content_hash: string;
  verdict: Verdict;
  reason_codes: string[];
  findings: Finding[];
  outline: { title: string; lessons: { title: string }[] }[];
  lesson_count: number;
  sampled_lessons: number;
  narration_included: boolean;
  created_at: string;
};

const base = (course: string) =>
  `/v1/courses/${encodeURIComponent(course)}/publication-drafts`;

/** The actor always comes from the session; the server decides ownership. */
export const drafts = {
  build: (course: string) =>
    session.request<PublicationDraft>(base(course), "POST", {}),
  get: (course: string, id: string) =>
    session.request<PublicationDraft>(
      `${base(course)}/${encodeURIComponent(id)}`,
    ),
  retry: (course: string, id: string) =>
    session.request<PublicationDraft>(
      `${base(course)}/${encodeURIComponent(id)}/screen`,
      "POST",
      {},
    ),
};

export type DraftFailure = "unavailable" | "offline";

export const draftMessages: Record<DraftFailure, string> = {
  unavailable: "This draft isn't available.",
  offline: "Couldn't reach Duolify. Check your connection and retry.",
};

/** Classifies by status only, never by server text; 404 is the one neutral refusal. */
export function draftFailure(error: unknown): DraftFailure {
  const status = (error as { status?: number }).status;
  return status === 404 ? "unavailable" : "offline";
}

export const verdictLabels: Record<Verdict, string> = {
  pass: "Passed screening",
  needs_review: "Needs review",
  block: "Blocked",
  pending_screening: "Screening pending",
};

export const findingLabels: Record<Finding["kind"], string> = {
  personal_data: "Personal data",
  secret: "Possible key or secret",
  out_of_allowlist: "Content that can't be shared",
};
