import { session } from "../accounts/session";

export type Verdict = "pass" | "needs_review" | "block" | "pending_screening";
export type VersionRow = {
  number: number;
  verdict: Verdict;
  live: boolean;
  lesson_count: number;
  changelog: string;
  created_at: string;
};
export type Versions = {
  versions: VersionRow[];
  republishes_remaining: number;
  limit: number;
};
export type Republished = {
  id: string;
  number: number;
  verdict: Verdict;
  lesson_count: number;
};
export type UpdateInfo = {
  update_available: boolean;
  forked: boolean;
  current_version: number;
  latest_version: number;
  lock_version: number;
  changelog: {
    version: number;
    text: string;
    added: number;
    removed: number;
    retitled: number;
  }[];
  completed_lessons: number;
  archived_count: number;
};
export type UpdateResult = {
  version: number;
  lock_version: number;
  kept: number;
  added: number;
  archived: number;
};
export type ArchivedLesson = {
  title: string;
  archived_at: string;
  version: number;
  completed: boolean;
};

export type RepublishFailure =
  "limit" | "not_published" | "adult" | "unavailable" | "offline";
export type UpdateFailure = "stale" | "unavailable" | "offline";

export const MAX_CHANGELOG = 500;
const course = (id: string) => `/v1/courses/${encodeURIComponent(id)}`;

/** The actor always comes from the session; the server decides ownership. */
export const republish = {
  versions: (id: string) => session.request<Versions>(`${course(id)}/versions`),
  publish: (id: string, changelog: string) =>
    session.request<Republished>(`${course(id)}/republish`, "POST", {
      changelog: changelog.trim(),
    }),
};

export const copyUpdates = {
  info: (id: string) => session.request<UpdateInfo>(`${course(id)}/update`),
  apply: (id: string, body: { lock_version: number; to_version: number }) =>
    session.request<UpdateResult>(`${course(id)}/update`, "POST", body),
  archived: async (id: string) =>
    (
      await session.request<{ lessons: ArchivedLesson[] }>(
        `${course(id)}/archived-lessons`,
      )
    ).lessons ?? [],
};

const status = (error: unknown) => (error as { status?: number }).status;

/** Classifies by status only, never by server text. */
export function republishFailure(error: unknown): RepublishFailure {
  switch (status(error)) {
    case 429:
      return "limit";
    case 409:
      return "not_published";
    case 403:
      return "adult";
    case 404:
      return "unavailable";
    default:
      return "offline";
  }
}

export function updateFailure(error: unknown): UpdateFailure {
  switch (status(error)) {
    case 409:
      return "stale";
    case 404:
      return "unavailable";
    default:
      return "offline";
  }
}

const offline = "Couldn't reach Duolify. Check your connection and retry.";
export const republishMessages: Record<RepublishFailure, string> = {
  limit: "You've reached today's limit for new versions. Try again tomorrow.",
  not_published:
    "Share this course with a link before publishing a new version.",
  adult: "Publishing needs an adult account.",
  unavailable: "This course isn't available to share.",
  offline,
};
export const updateMessages: Record<UpdateFailure, string> = {
  stale:
    "This course changed while you were updating. Review the latest version and try again.",
  unavailable: "This update isn't available.",
  offline,
};
