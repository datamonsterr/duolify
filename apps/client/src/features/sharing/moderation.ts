export type ModerationAction =
  "dismiss" | "warn" | "delist" | "takedown" | "ban_publishing";

export const severityLabel = (_severity: number): "High" | "Medium" | "Low" =>
  "Low";
export const reasonCodes: { id: string; label: string }[] = [];
export const actionsFor = (_targetType: string): ModerationAction[] => [];
export const isSevereAction = (_action: string, _code: string) => false;
export const moderationFailure = (_error: unknown): string => "";
