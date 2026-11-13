import { session } from "../accounts/session";
export const briefFields = [
  { id: "topic", label: "Topic" },
  { id: "goal", label: "Goal" },
  { id: "include_topics", label: "Include topics" },
  { id: "skip_topics", label: "Skip topics" },
  { id: "level", label: "Depth / level" },
  { id: "maths", label: "Maths" },
  { id: "viewing_conditions", label: "Viewing conditions" },
  { id: "lesson_length", label: "Lesson length" },
  { id: "language", label: "Language" },
] as const;
export type BriefField = (typeof briefFields)[number]["id"];
export type Brief = Record<BriefField, string>;
export const emptyBrief = (): Brief =>
  Object.fromEntries(briefFields.map((f) => [f.id, ""])) as Brief;
export const questions: Record<BriefField, string> = {
  topic: "What would you like to learn?",
  goal: "What is your goal?",
  include_topics: "Which topics should we include?",
  skip_topics: "Which topics should we skip?",
  level: "What depth or level suits you?",
  maths: "How much maths would you like?",
  viewing_conditions: "Where will you use what you learn?",
  lesson_length: "How long should each lesson be?",
  language: "Which language should the course use?",
};
export type BriefRevision = {
  id: string;
  content_hash: string;
  brief: Brief;
  expires_at?: string;
};
export type BriefTurn = {
  brief: Brief;
  current_step: BriefField;
  answer: string;
  skipped: boolean;
  is_follow_up: boolean;
};
export type BriefTurnResult = {
  brief: Brief;
  message: string;
  quick_replies?: { id: string; label: string }[];
  next_step: string;
  is_follow_up: boolean;
  is_complete?: boolean;
};
export type BriefConfig = {
  enabled: boolean;
  credits: number | null;
  pricing_version: string;
};
export interface BriefService {
  config(): Promise<BriefConfig>;
  chat(turn: BriefTurn): Promise<BriefTurnResult>;
  confirm(brief: Brief): Promise<BriefRevision>;
}
export const briefApi: BriefService = {
  config: () => session.request("/v1/course-brief/config"),
  chat: (turn) => session.request("/v1/course-brief/chat", "POST", turn),
  confirm: (brief) =>
    session.request("/v1/course-brief/confirm", "POST", {
      brief,
      confirmed: true,
    }),
};
export function seedBrief(profile: {
  topic: string;
  goal: string;
  level: string;
  locale: string;
  setup_completed: boolean;
}): Brief {
  const b = emptyBrief();
  if (
    profile.setup_completed &&
    (profile.topic || profile.goal || profile.level)
  ) {
    b.topic = profile.topic;
    b.goal = profile.goal;
    b.level = profile.level;
    b.language = profile.locale;
  }
  return b;
}
