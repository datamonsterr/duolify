import { courseBrief } from "./brief.script";
import { courseLevel, courseSteer } from "./course.scripts";
import { onboardingSetup } from "./onboarding.script";
import type { GuidedScript } from "./types";

const opts = (prefix: string, ids: string[]) =>
  ids.map((id) => ({ id, labelKey: `${prefix}.${id}` }));

/** Authored fixture: exercises every widget. Not shipped as a Learner flow. */
export const fixtureTour: GuidedScript = {
  id: "fixture.tour",
  version: 1,
  entryPoints: ["fixture"],
  flag: "FEATURE_GUIDED_FIXTURE",
  steps: [
    {
      id: "name",
      widget: "text",
      promptKey: "fixture.name.q",
      validate: { type: "string", max: 80 },
      persist: true,
    },
    {
      id: "level",
      widget: "single",
      promptKey: "fixture.level.q",
      options: opts("fixture.level", ["beginner", "some", "expert"]),
      validate: { type: "enum" },
      persist: true,
      skippable: true,
    },
    {
      id: "goals",
      widget: "multi",
      promptKey: "fixture.goals.q",
      options: opts("fixture.goals", ["speak", "read", "write"]),
      validate: { type: "multi", maxItems: 3 },
      persist: true,
      skippable: true,
    },
    {
      id: "remind",
      widget: "toggle",
      promptKey: "fixture.remind.q",
      validate: { type: "boolean" },
      persist: true,
      next: [{ when: { eq: ["remind", false] }, to: "date" }],
    },
    {
      id: "time",
      widget: "time",
      promptKey: "fixture.time.q",
      validate: { type: "hhmm" },
      persist: true,
      when: { eq: ["remind", true] },
    },
    {
      id: "date",
      widget: "date",
      promptKey: "fixture.date.q",
      validate: { type: "date" },
      persist: true,
    },
    {
      id: "minutes",
      widget: "number",
      promptKey: "fixture.minutes.q",
      validate: { type: "number", min: 1, max: 1440, integer: true },
      limit: "profile.daily_minutes",
      persist: true,
    },
    {
      id: "notes",
      widget: "longtext",
      promptKey: "fixture.notes.q",
      validate: { type: "string", max: 2000 },
      limit: "profile.text",
      persist: true,
      skippable: true,
    },
    {
      id: "photo",
      widget: "file",
      promptKey: "fixture.photo.q",
      validate: {
        type: "file",
        maxBytes: 2_000_000,
        accept: ["image/png", "image/jpeg"],
      },
      persist: false,
      skippable: true,
    },
    {
      id: "rating",
      widget: "rating",
      promptKey: "fixture.rating.q",
      validate: { type: "rating", max: 5 },
      persist: true,
    },
    {
      id: "confirm",
      widget: "confirm",
      promptKey: "fixture.confirm.q",
      summaryOf: [
        "name",
        "level",
        "goals",
        "remind",
        "time",
        "date",
        "minutes",
        "rating",
      ],
      persist: false,
      submit: "PUT /v1/me/profile",
    },
  ],
};

/** Authored fixture for secure inputs and a destructive confirm card. */
export const fixtureSecure: GuidedScript = {
  id: "fixture.secure",
  version: 1,
  entryPoints: ["fixture"],
  flag: "FEATURE_GUIDED_FIXTURE",
  steps: [
    {
      id: "email",
      widget: "email",
      promptKey: "fixture.email.q",
      validate: { type: "email" },
      persist: false,
      input: { name: "email", autocomplete: "email", inputMode: "email" },
    },
    {
      id: "code",
      widget: "code",
      promptKey: "fixture.code.q",
      validate: { type: "code", length: 6 },
      limit: "auth.code",
      persist: false,
      input: {
        name: "code",
        autocomplete: "one-time-code",
        inputMode: "numeric",
      },
    },
    {
      id: "password",
      widget: "password",
      promptKey: "fixture.password.q",
      validate: { type: "string", max: 256 },
      persist: false,
      input: { name: "password", autocomplete: "current-password" },
    },
    {
      id: "confirm",
      widget: "confirm",
      promptKey: "fixture.delete.q",
      destructive: true,
      summaryOf: [],
      persist: false,
      submit: "DELETE /v1/me",
    },
  ],
};

export const allScripts: GuidedScript[] = [
  fixtureTour,
  fixtureSecure,
  onboardingSetup,
  courseBrief,
  courseLevel,
  courseSteer,
];
