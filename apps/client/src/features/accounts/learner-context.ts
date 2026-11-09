import { authRequest, credentialStore, session } from "./session";

export const contextFields = [
  { id: "goal", label: "Goal", kind: "text" },
  { id: "topic", label: "Topic", kind: "text" },
  { id: "background", label: "Background", kind: "text" },
  { id: "level", label: "Level", kind: "level" },
  { id: "daily_minutes", label: "Daily minutes", kind: "number", max: 1440 },
  { id: "daily_xp", label: "Daily XP", kind: "number", max: 10000 },
  { id: "reminder_enabled", label: "Reminder", kind: "reminder" },
  { id: "reminder_time", label: "Reminder time", kind: "time" },
  { id: "locale", label: "Locale", kind: "locale" },
  { id: "time_zone", label: "Time zone", kind: "zone" },
] as const;
export type ContextFields = {
  goal: string;
  topic: string;
  background: string;
  level: string;
  daily_minutes: number | null;
  daily_xp: number | null;
  reminder_enabled: boolean | null;
  reminder_time: string;
  locale: string;
  time_zone: string;
};
export type LearnerContext = ContextFields & {
  learner_id: string;
  setup_completed: boolean;
  text_limit: number;
};
export const emptyContext = (): LearnerContext => ({
  learner_id: "",
  goal: "",
  topic: "",
  background: "",
  level: "",
  daily_minutes: null,
  daily_xp: null,
  reminder_enabled: null,
  reminder_time: "",
  locale: navigator.language || "en",
  time_zone: Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC",
  setup_completed: false,
  text_limit: 2000,
});
export function contextValues(profile: ContextFields): ContextFields {
  return Object.fromEntries(
    contextFields.map(({ id }) => [id, profile[id]]),
  ) as ContextFields;
}
export function validateContext(
  draft: ContextFields,
  limit: number,
): Partial<Record<keyof ContextFields, string>> {
  const errors: Partial<Record<keyof ContextFields, string>> = {};
  for (const key of ["goal", "topic", "background"] as const)
    if (Array.from(draft[key]).length > limit)
      errors[key] = `Use at most ${limit} characters.`;
  if (
    !["", "beginner", "intermediate", "advanced", "expert"].includes(
      draft.level,
    )
  )
    errors.level = "Choose a listed level.";
  for (const [key, max] of [
    ["daily_minutes", 1440],
    ["daily_xp", 10000],
  ] as const) {
    const value = draft[key];
    if (
      value !== null &&
      (!Number.isInteger(value) || value < 1 || value > max)
    )
      errors[key] = `Enter a whole number from 1 to ${max}, or leave blank.`;
  }
  if (
    draft.reminder_time &&
    !/^([01][0-9]|2[0-3]):[0-5][0-9]$/.test(draft.reminder_time)
  )
    errors.reminder_time = "Enter time as HH:MM.";
  if (
    draft.locale &&
    (!/^[A-Za-z]{2,8}(-[A-Za-z0-9]{1,8})*$/.test(draft.locale) ||
      draft.locale.length > 64)
  )
    errors.locale = "Enter a language tag such as en-US.";
  if (draft.time_zone) {
    try {
      if (draft.time_zone === "Local" || draft.time_zone.length > 64)
        throw Error();
      new Intl.DateTimeFormat("en", { timeZone: draft.time_zone });
    } catch {
      errors.time_zone = "Enter an IANA time zone such as Asia/Ho_Chi_Minh.";
    }
  }
  return errors;
}
async function accessToken() {
  await session.load();
  const tokens = await credentialStore.read();
  if (!tokens) throw new Error("Sign in again to edit your profile.");
  return tokens.access_token;
}
export const profileApi = {
  async read() {
    return authRequest<LearnerContext>(
      "/v1/me/profile",
      undefined,
      await accessToken(),
    );
  },
  async commit(action: "save" | "skip", fields: Partial<ContextFields>) {
    return authRequest<LearnerContext>(
      "/v1/me/profile",
      { confirmed: true, action, fields },
      await accessToken(),
    );
  },
  async export() {
    return authRequest<LearnerContext>(
      "/v1/me/profile/export",
      undefined,
      await accessToken(),
    );
  },
};
