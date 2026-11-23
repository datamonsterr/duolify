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

const plural = (n: number) => `${n} ${n === 1 ? "day" : "days"}`;

/** Status of one friend's own local day. */
export function sideStatus(side: DaySide): string {
  if (side.done) return "done";
  return side.protected ? "covered" : "pending";
}

function checkingNote(s: FriendStreak): string {
  const until = s.checking_until ? new Date(s.checking_until) : undefined;
  return until && !Number.isNaN(until.getTime())
    ? `Shared day checking until ${until.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}`
    : "Shared day checking";
}

/** Plain-text summary; the components add bidi isolation around the friend's name. */
export function describeStreak(name: string, s: FriendStreak): StreakText {
  const headline =
    s.state === "none"
      ? "Start a friend streak"
      : s.state === "broken"
        ? `Friend streak ended · longest ${plural(s.longest)}`
        : `Friend streak · ${plural(s.days)}`;
  let note = "";
  if (s.state === "both_done") note = "Both done today";
  else if (s.state === "checking") note = checkingNote(s);
  else if (s.state === "protected")
    note =
      s.you.protected && s.friend.protected
        ? "Both freezes covered today"
        : s.you.protected
          ? "Your freeze covered today"
          : `${name}'s freeze covered today`;
  return {
    headline,
    you: `You: ${sideStatus(s.you)}`,
    friend: `${name}: ${sideStatus(s.friend)}`,
    note,
  };
}
