import "./messages.setup";
import "./messages.profile";
import type { GuidedScript } from "./types";

const o = (prefix: string, ids: string[]) =>
  ids.map((id) => ({ id, labelKey: `${prefix}.${id}` }));

/** Profile edit: the learner-context text fields as a chat. Submits to `POST /v1/me/profile`. */
export const profileEdit: GuidedScript = {
  id: "profile.edit",
  version: 1,
  entryPoints: ["profile"],
  flag: "FEATURE_GUIDED_REST",
  steps: [
    {
      id: "goal",
      widget: "chips+text",
      promptKey: "g.setup.goal.q",
      options: o("g.setup.goal", ["career", "school", "hobby", "travel"]),
      validate: { type: "string", max: 2000 },
      limit: "profile.text",
      skippable: true,
      persist: true,
      analyticsId: "goal",
    },
    {
      id: "topic",
      widget: "text",
      promptKey: "g.profile.topic.q",
      validate: { type: "string", max: 2000 },
      limit: "profile.text",
      skippable: true,
      persist: true,
      analyticsId: "topic",
    },
    {
      id: "background",
      widget: "longtext",
      promptKey: "g.profile.background.q",
      validate: { type: "string", max: 2000 },
      limit: "profile.text",
      skippable: true,
      persist: true,
      analyticsId: "background",
    },
    {
      id: "level",
      widget: "single",
      promptKey: "g.setup.level.q",
      options: o("g.setup.level", [
        "beginner",
        "intermediate",
        "advanced",
        "expert",
      ]),
      validate: { type: "enum" },
      skippable: true,
      persist: true,
      analyticsId: "level",
    },
    {
      id: "confirm",
      widget: "confirm",
      promptKey: "g.setup.confirm.q",
      summaryOf: ["goal", "topic", "background", "level"],
      persist: false,
      submit: "POST /v1/me/profile",
      analyticsId: "confirm",
    },
  ],
};

/** Daily goal and reminder: minutes, XP, reminder switch and the clock. Submits to settings. */
export const profileGoal: GuidedScript = {
  id: "profile.goal",
  version: 1,
  entryPoints: ["settings"],
  flag: "FEATURE_GUIDED_REST",
  steps: [
    {
      id: "daily_minutes",
      widget: "number",
      promptKey: "g.setup.minutes.q",
      initial: 15,
      validate: { type: "number", min: 1, max: 1440, integer: true },
      limit: "profile.daily_minutes",
      skippable: true,
      persist: true,
      analyticsId: "minutes",
    },
    {
      id: "daily_xp",
      widget: "number",
      promptKey: "g.goal.xp.q",
      initial: 50,
      validate: { type: "number", min: 1, max: 10000, integer: true },
      limit: "profile.daily_xp",
      skippable: true,
      persist: true,
      analyticsId: "xp",
    },
    {
      id: "reminder_on",
      widget: "toggle",
      promptKey: "g.setup.remind.q",
      validate: { type: "boolean" },
      persist: true,
      analyticsId: "reminder",
      next: [{ when: { eq: ["reminder_on", false] }, to: "confirm" }],
    },
    {
      id: "reminder_time",
      widget: "time",
      promptKey: "g.setup.time.q",
      initial: "09:00",
      validate: { type: "hhmm" },
      persist: true,
      when: { eq: ["reminder_on", true] },
      analyticsId: "reminder_time",
    },
    {
      id: "confirm",
      widget: "confirm",
      promptKey: "g.goal.confirm.q",
      summaryOf: ["daily_minutes", "daily_xp", "reminder_on", "reminder_time"],
      persist: false,
      submit: "PATCH /v1/me/settings",
      analyticsId: "confirm",
    },
  ],
};
