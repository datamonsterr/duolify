import type { FriendStreak } from "./friendStreak";
export interface FriendAvatar {
  initial: string;
}
export interface FriendEntry {
  id: string;
  username: string;
  display_name: string;
  avatar: FriendAvatar;
  since: string;
  streak?: { days: number };
  friend_streak?: FriendStreak;
}
export interface RequestEntry {
  id: string;
  username: string;
  display_name: string;
  avatar: FriendAvatar;
  created_at: string;
}
export interface FriendRequests {
  incoming: RequestEntry[];
  outgoing: RequestEntry[];
}
export interface SendResult {
  status: "pending" | "friends";
  id?: string;
}

export const friendsPath = "/v1/social/friends";
export const requestsPath = "/v1/social/friend-requests";
export const linkPath = "/v1/social/friend-link";
export const tokenLength = 43;

export type FriendFailure =
  | "unavailable"
  | "invite"
  | "pending_limit"
  | "friend_limit"
  | "limited"
  | "offline";

/** Neutral copy only: nothing here says why a request was refused. */
export const friendMessages: Record<FriendFailure, string> = {
  unavailable: "We couldn't send that request.",
  invite: "This invite has expired. Ask for a new one.",
  pending_limit:
    "You have too many requests waiting. Cancel one or wait for a reply.",
  friend_limit: "A friends list is full, so this can't be completed.",
  limited: "Too many tries. Wait a moment and try again.",
  offline: "Couldn't reach Duolify. Check your connection and retry.",
};

/** Classifies by status and stable code, never by server text. */
export function friendFailure(error: unknown): FriendFailure {
  const e = error as {
    status?: number;
    code?: string;
    details?: { code?: string };
  };
  const code = e.code ?? e.details?.code;
  if (e.status === 429) return "limited";
  if (e.status === 404 && code === "invite_unavailable") return "invite";
  if (e.status === 404 && code === "request_unavailable") return "unavailable";
  if (e.status === 409 && code === "pending_limit") return "pending_limit";
  if (e.status === 409 && code === "friend_limit") return "friend_limit";
  if (e.status === 409 || e.status === 404) return "unavailable";
  return "offline";
}

export function inviteUrl(origin: string, token: string): string {
  return `${origin}/friends/invite/${token}`;
}

/** Same alphabet and length the API accepts, so bad links never reach the network. */
export function parseInviteToken(raw: string): string | null {
  return raw.length === tokenLength && /^[A-Za-z0-9_-]+$/.test(raw)
    ? raw
    : null;
}

export function sinceLabel(iso: string): string {
  const date = new Date(iso);
  return Number.isNaN(date.getTime())
    ? ""
    : date.toLocaleDateString(undefined, { year: "numeric", month: "long" });
}
