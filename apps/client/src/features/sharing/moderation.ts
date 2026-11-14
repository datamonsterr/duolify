import { session } from "../accounts/session";

// ModerationAction covers human moderator actions available in the queue dialog.
// The system audit action "restore_link" is deliberately excluded: it is recorded
// automatically on appeal overturn and is never an action dialog choice.
export type ModerationAction =
  "dismiss" | "warn" | "delist" | "takedown" | "ban_publishing";
export type TargetType = "course" | "publication" | "review" | "user";

export type AuditEntry = { at: string; action: string; reason_code: string };
export type QueueItem = {
  id: string;
  target_type: TargetType;
  target_id: string;
  course_id: string;
  title: string;
  severity: number;
  reasons: string[];
  report_count: number;
  opened_at: string;
  due_at: string;
  overdue: boolean;
  hidden: boolean;
  version: number;
  screening?: { verdict: string; codes: string[]; sampled_lessons: number };
  audit: AuditEntry[];
};
export type AppealItem = {
  id: string;
  action_id: string;
  course_id: string;
  title: string;
  action: string;
  reason_code: string;
  text: string;
  created_at: string;
};
export type QueueView = {
  items: QueueItem[];
  appeals: AppealItem[];
  open_count: number;
  overdue_count: number;
  screening_count: number;
};
export type Notice = {
  id: string;
  action: ModerationAction;
  reason_code: string;
  reason: string;
  target_type: TargetType;
  course_id: string;
  title: string;
  severe: boolean;
  created_at: string;
  can_appeal: boolean;
  appeal?: { status: "open" | "upheld" | "overturned" };
};

/** Same closed list and order as the API (`SHARING_MODERATION_REASON_CODES`). */
export const reasonCodes = [
  { id: "unsafe", label: "Unsafe content" },
  { id: "legal", label: "Legal reasons" },
  { id: "inappropriate", label: "Inappropriate content" },
  { id: "spam", label: "Spam" },
  { id: "copyright", label: "Copyright" },
  { id: "misleading", label: "Misleading" },
  { id: "other", label: "Other" },
  { id: "no_violation", label: "No violation" },
];
/** Codes whose takedown also blocks existing copies; the API decides, this only previews it. */
const severeCodes = ["unsafe", "legal"];
export const defaultTargetHours = 48;
export const reasonMaxChars = 1000;

const matrix: Record<string, ModerationAction[]> = {
  course: ["dismiss", "warn", "delist", "takedown", "ban_publishing"],
  publication: ["dismiss", "delist", "takedown", "ban_publishing"],
  review: ["dismiss", "warn", "delist", "takedown"],
  user: ["dismiss", "warn", "ban_publishing"],
};
export const actionsFor = (targetType: string): ModerationAction[] =>
  matrix[targetType] ?? [];
export const isSevereAction = (action: string, code: string) =>
  action === "takedown" && severeCodes.includes(code);
export const severityLabel = (severity: number): "High" | "Medium" | "Low" =>
  severity >= 80 ? "High" : severity >= 40 ? "Medium" : "Low";

export const targetLabels: Record<string, string> = {
  course: "Course",
  publication: "Course",
  review: "Review",
  user: "User",
};
/** Button and dialog wording per target, because "dismiss" means "approve" for a held publication. */
export function actionLabel(targetType: string, action: ModerationAction) {
  const held = targetType === "publication";
  switch (action) {
    case "dismiss":
      return held ? "Approve publication" : "Dismiss";
    case "warn":
      return targetType === "course"
        ? "Warn owner"
        : targetType === "review"
          ? "Warn author"
          : "Warn user";
    case "delist":
      return held
        ? "Reject publication"
        : targetType === "review"
          ? "Hide review"
          : "Delist";
    case "takedown":
      return "Take down";
    case "ban_publishing":
      return "Ban publishing";
  }
}
export function dialogTitle(targetType: string, action: ModerationAction) {
  const noun = targetType === "review" ? "review" : "course";
  switch (action) {
    case "dismiss":
      return targetType === "publication"
        ? "Approve publication"
        : "Dismiss reports";
    case "warn":
      return "Send warning";
    case "delist":
      return targetType === "publication"
        ? "Reject publication"
        : targetType === "review"
          ? "Hide review"
          : "Delist course";
    case "takedown":
      return `Take down ${noun}`;
    case "ban_publishing":
      return "Ban publishing";
  }
}
export const auditWords: Record<string, string> = {
  dismiss: "Dismissed",
  warn: "Warned",
  delist: "Delisted",
  takedown: "Took down",
  ban_publishing: "Banned publishing",
  restore_link: "Share link restored",
};

export function noticeLabel(n: Pick<Notice, "target_type" | "action">) {
  const names: Record<string, Record<string, string>> = {
    course: {
      warn: "course warning",
      delist: "course delisted",
      takedown: "course taken down",
      ban_publishing: "publishing ban",
    },
    publication: {
      delist: "publication rejected",
      takedown: "publication rejected",
      ban_publishing: "publishing ban",
    },
    review: {
      warn: "review warning",
      delist: "review hidden",
      takedown: "review removed",
    },
    user: { warn: "account warning", ban_publishing: "publishing ban" },
  };
  return names[n.target_type]?.[n.action] ?? "decision";
}

/** Status only, never server text. */
export function moderationFailure(error: unknown): string {
  switch ((error as { status?: number }).status) {
    case 404:
      return "This isn't available anymore.";
    case 403:
      return "You can't act on this one.";
    case 409:
      return "The one appeal for this decision is already used.";
    case 429:
      return "Too many requests. Wait a minute, then retry.";
    case 400:
      return "Check the reason and try again.";
    case undefined:
      return "Couldn't reach Duolify. Check your connection and retry.";
    default:
      return "Something went wrong on our side. Try again.";
  }
}
export const isUnavailable = (error: unknown) =>
  (error as { status?: number }).status === 404;

export const fetchQueue = () =>
  session.request<QueueView>("/v1/moderation/queue");
export const sendAction = (
  itemId: string,
  action: ModerationAction,
  reasonCode: string,
  reason: string,
) =>
  session.request<{ id: string }>(
    `/v1/moderation/items/${encodeURIComponent(itemId)}/actions`,
    "POST",
    { action, reason_code: reasonCode, reason },
  );
export const decideAppeal = (
  id: string,
  decision: "uphold" | "overturn",
  reason: string,
) =>
  session.request<void>(
    `/v1/moderation/appeals/${encodeURIComponent(id)}/decision`,
    "POST",
    { decision, reason },
  );
export const fetchNotices = () =>
  session.request<Notice[]>("/v1/moderation/notices");
export const fileAppeal = (actionId: string, text: string) =>
  session.request<{ id: string }>(
    `/v1/moderation/notices/${encodeURIComponent(actionId)}/appeal`,
    "POST",
    { text },
  );
