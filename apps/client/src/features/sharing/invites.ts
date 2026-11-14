import { session } from "../accounts/session";

export type InviteStatus =
  "pending" | "accepted" | "declined" | "expired" | "revoked";
export interface InvitePerson {
  username: string;
  display_name: string;
  avatar: { initial: string };
}
export interface SentInvite {
  id: string;
  status: InviteStatus;
  method: "username" | "friend";
  created_at: string;
  expires_at: string;
  recipient: InvitePerson;
}
export interface ReceivedInvite {
  id: string;
  created_at: string;
  expires_at: string;
  read: boolean;
  /** Only the title: the recipient sees no other course material before accepting. */
  course: { title: string };
  sender: InvitePerson;
}
export interface InviteInbox {
  invites: ReceivedInvite[];
  unread: number;
}
export type InviteTarget = { username: string } | { friendship_id: string };
export type InviteFailure =
  "not_shared" | "unavailable" | "limited" | "duplicate" | "offline";

/** Neutral copy: a refusal never says why (block, setting, unknown or hidden name). */
export const inviteMessages: Record<InviteFailure, string> = {
  not_shared: "Share this course with a link before you invite anyone.",
  unavailable: "We couldn't send that invite.",
  limited: "Daily invite limit reached. You can send more invites tomorrow.",
  duplicate: "You already have a pending invite for them to this course.",
  offline: "Check your connection and try again.",
};
export const gone = "This invite isn't available any more.";

/** Classifies by status and error code, never by server text. */
export function inviteFailure(error: unknown): InviteFailure {
  const e = error as {
    status?: number;
    code?: string;
    details?: { code?: string; error?: string | { code?: string } };
  };
  // The API sends flat `{"error":"<code>"}`; nested and bare codes are tolerated.
  const flat = e.details?.error;
  const code =
    e.code ?? (typeof flat === "string" ? flat : flat?.code) ?? e.details?.code;
  if (e.status === 429 || code === "invite_rate_limited") return "limited";
  if (code === "course_not_shared") return "not_shared";
  if (code === "invite_duplicate") return "duplicate";
  if (e.status === 404 || e.status === 409) return "unavailable";
  return "offline";
}

export const cleanName = (raw: string) =>
  raw.trim().replace(/^@+/, "").toLowerCase();

const course = (id: string) => `/v1/courses/${id}/invites`;

export const invites = {
  send: (courseId: string, target: InviteTarget, operation_key: string) =>
    session.request<{ id: string; status: "pending"; expires_at: string }>(
      course(courseId),
      "POST",
      { ...target, operation_key },
    ),
  sent: (courseId: string) =>
    session.request<{ invites: SentInvite[] }>(course(courseId)),
  received: () => session.request<InviteInbox>("/v1/invites/received"),
  markRead: () => session.request<void>("/v1/invites/read", "POST"),
  accept: (id: string) =>
    session.request<{ status: "accepted"; course_id: string }>(
      `/v1/invites/${id}/accept`,
      "POST",
    ),
  decline: (id: string) =>
    session.request<{ status: string }>(`/v1/invites/${id}/decline`, "POST"),
  revoke: (id: string) =>
    session.request<{ status: string }>(`/v1/invites/${id}/revoke`, "POST"),
};
