import type { GuidedScript } from "./types";

const level = ["beginner", "intermediate", "advanced", "expert"].map((id) => ({
  id,
  labelKey: `g.setup.level.${id}`,
}));
const goal = ["career", "school", "hobby", "travel"].map((id) => ({
  id,
  labelKey: `g.setup.goal.${id}`,
}));

/** Onboarding and "update by chat". Submits to the existing profile endpoint. */
export const onboardingSetup: GuidedScript = {
  id: "onboarding.setup",
  version: 1,
  entryPoints: ["onboarding", "profile"],
  flag: "FEATURE_GUIDED_ONBOARDING",
  steps: [
    {
      id: "goal",
      widget: "chips+text",
      promptKey: "g.setup.goal.q",
      options: goal,
      validate: { type: "string", max: 2000 },
      limit: "profile.text",
      skippable: true,
      persist: true,
      analyticsId: "goal",
    },
    {
      id: "level",
      widget: "single",
      promptKey: "g.setup.level.q",
      options: level,
      validate: { type: "enum" },
      skippable: true,
      persist: true,
      analyticsId: "level",
    },
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
      id: "reminder_on",
      widget: "toggle",
      promptKey: "g.setup.remind.q",
      validate: { type: "boolean" },
      skippable: true,
      persist: true,
      analyticsId: "reminder",
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
      promptKey: "g.setup.confirm.q",
      summaryOf: [
        "goal",
        "level",
        "daily_minutes",
        "reminder_on",
        "reminder_time",
      ],
      persist: false,
      submit: "POST /v1/me/profile",
      analyticsId: "confirm",
    },
  ],
};
