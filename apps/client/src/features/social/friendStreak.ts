export interface DaySide {
  local_date: string;
  time_zone: string;
  done: boolean;
  protected: boolean;
}
export type StreakState =
  | "none"
  | "waiting_you"
  | "waiting_friend"
  | "waiting_both"
  | "both_done"
  | "protected"
  | "checking"
  | "broken";
export interface FriendStreak {
  days: number;
  longest: number;
  state: StreakState;
  you: DaySide;
  friend: DaySide;
  checking_until?: string;
}
export interface StreakText {
  headline: string;
  you: string;
  friend: string;
  note: string;
}

export function describeStreak(_name: string, _s: FriendStreak): StreakText {
  return { headline: "", you: "", friend: "", note: "" };
}
