// Skeleton so the red tests compile; replaced by the real module.
export interface BlockedEntry {
  username: string;
  display_name: string;
  avatar: { initial: string };
  blocked_at: string;
}
export const blocksPath = "/v1/social/blocks";
export type BlockFailure = "unavailable" | "limit" | "limited" | "offline";
export const blockMessages: Record<BlockFailure, string> = {
  unavailable: "",
  limit: "",
  limited: "",
  offline: "",
};
export function blockFailure(_error: unknown): BlockFailure {
  return "offline";
}
