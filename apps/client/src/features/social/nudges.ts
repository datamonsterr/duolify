import { friendMessages, type FriendEntry } from "./friends";

export interface NudgeEntry {
  id: string;
  username: string;
  display_name: string;
  avatar: { initial: string };
  created_at: string;
  read: boolean;
}
export interface NudgeInbox {
  nudges: NudgeEntry[];
  unread: number;
}
export type NudgeStateName = "available" | "sent" | "finished";
export type NudgeFailure = "limited" | "unavailable" | "offline";

export const nudgesPath = "/v1/social/nudges";
export const nudgesReadPath = `${nudgesPath}/read`;
export const nudgePath = (friendshipId: string) =>
  `/v1/social/friends/${friendshipId}/nudge`;

/** Sent wins: the day's nudge is spent. A visible finished day needs no nudge. */
export function nudgeState(entry: FriendEntry): NudgeStateName {
  if (entry.nudge?.sent) return "sent";
  return entry.friend_streak?.friend.done ? "finished" : "available";
}

export function nudgeNote(state: NudgeStateName, name: string): string {
  if (state === "sent") return "Nudge sent · you can nudge again tomorrow";
  return state === "finished" ? `${name} finished today` : "";
}

/** Neutral copy: a refusal never says why (block, removal, setting or deletion). */
export function nudgeMessage(kind: NudgeFailure, name: string): string {
  if (kind === "limited")
    return `You already nudged ${name} today. You can nudge again tomorrow.`;
  return kind === "unavailable"
    ? "We couldn't send that nudge."
    : friendMessages.offline;
}

/** Classifies by status, never by server text. */
export function nudgeFailure(error: unknown): NudgeFailure {
  const status = (error as { status?: number }).status;
  if (status === 429) return "limited";
  return status === 404 ? "unavailable" : "offline";
}

export function unreadCount(items: NudgeEntry[]): number {
  return items.filter((item) => !item.read).length;
}

/** One operation key per friend until a send settles, so a retry replays, never repeats. */
export function createNudgeKeys(
  generate: () => string = () => crypto.randomUUID(),
) {
  const keys = new Map<string, string>();
  return {
    keyFor(id: string): string {
      let key = keys.get(id);
      if (!key) {
        key = generate();
        keys.set(id, key);
      }
      return key;
    },
    clear(id: string) {
      keys.delete(id);
    },
  };
}
