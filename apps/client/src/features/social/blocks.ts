export interface BlockedEntry {
  username: string;
  display_name: string;
  avatar: { initial: string };
  blocked_at: string;
}

export const blocksPath = "/v1/social/blocks";

export type BlockFailure = "unavailable" | "limit" | "limited" | "offline";

/** One neutral line for every refusal: nothing says why a person can't be blocked. */
export const blockMessages: Record<BlockFailure, string> = {
  unavailable: "We couldn't block that person.",
  limit: "Your blocked list is full. Unblock someone first.",
  limited: "Too many tries. Wait a moment and try again.",
  offline: "Couldn't reach Duolify. Check your connection and retry.",
};

/** Classifies by status and stable code, never by server text. */
export function blockFailure(error: unknown): BlockFailure {
  const e = error as {
    status?: number;
    code?: string;
    details?: { code?: string };
  };
  const code = e.code ?? e.details?.code;
  if (e.status === 429) return "limited";
  if (e.status === 404 && code === "block_unavailable") return "unavailable";
  if (e.status === 409 && code === "block_limit") return "limit";
  return "offline";
}

/** The username as typed: trimmed, without one leading "@". */
export function cleanUsername(raw: string): string {
  return raw.trim().replace(/^@/, "");
}
