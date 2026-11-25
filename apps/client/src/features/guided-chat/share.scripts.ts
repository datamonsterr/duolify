import "./messages.share";
import type { GuidedScript, Step } from "./types";

const opts = (prefix: string, ids: string[]) =>
  ids.map((id) => ({ id, labelKey: `${prefix}.${id}` }));

const confirm = (
  promptKey: string,
  actionKey: string,
  submit: string,
  summaryOf: string[] = [],
): Step => ({
  id: "confirm",
  widget: "confirm",
  promptKey,
  summaryOf,
  actionKey,
  persist: false,
  submit,
  analyticsId: "confirm",
});

/** Who can open the course: private or an unlisted link. Same publish and revoke calls. */
export const courseShare: GuidedScript = {
  id: "course.share",
  version: 1,
  entryPoints: ["share"],
  flag: "FEATURE_GUIDED_REST",
  steps: [
    {
      id: "visibility",
      widget: "single",
      promptKey: "g.share.q",
      options: opts("g.share.v", ["private", "link"]),
      validate: { type: "enum" },
      persist: true,
      analyticsId: "visibility",
    },
    confirm(
      "g.share.confirm.q",
      "g.share.action",
      "POST /v1/courses/:id/share-link",
      ["visibility"],
    ),
  ],
};

/** Invite one friend by exact username. A refusal never says why. */
export const courseInvite: GuidedScript = {
  id: "course.invite",
  version: 1,
  entryPoints: ["share"],
  flag: "FEATURE_GUIDED_REST",
  steps: [
    {
      id: "username",
      widget: "text",
      promptKey: "g.invite.q",
      validate: { type: "string", min: 1, max: 32 },
      persist: false,
      input: {
        name: "invite-username",
        autocomplete: "off",
      },
      analyticsId: "username",
    },
    confirm(
      "g.invite.confirm.q",
      "g.invite.action",
      "POST /v1/courses/:id/invites",
      ["username"],
    ),
  ],
};

/** A new immutable version with an optional changelog. Screening is unchanged. */
export const courseRepublish: GuidedScript = {
  id: "course.republish",
  version: 1,
  entryPoints: ["share"],
  flag: "FEATURE_GUIDED_REST",
  steps: [
    {
      id: "changelog",
      widget: "longtext",
      promptKey: "g.republish.q",
      validate: { type: "string", max: 500 },
      persist: false,
      skippable: true,
      analyticsId: "changelog",
    },
    confirm(
      "g.republish.confirm.q",
      "g.republish.action",
      "POST /v1/courses/:id/republish",
      ["changelog"],
    ),
  ],
};

/** Star rating plus an optional note. Posting again replaces the earlier review. */
export const courseReview: GuidedScript = {
  id: "course.review",
  version: 1,
  entryPoints: ["marketplace"],
  flag: "FEATURE_GUIDED_REST",
  steps: [
    {
      id: "stars",
      widget: "rating",
      promptKey: "g.review.stars.q",
      validate: { type: "rating", max: 5 },
      persist: true,
      analyticsId: "stars",
    },
    {
      id: "text",
      widget: "longtext",
      promptKey: "g.review.text.q",
      validate: { type: "string", max: 2000 },
      limit: "review.text",
      persist: true,
      skippable: true,
      analyticsId: "text",
    },
    confirm(
      "g.review.confirm.q",
      "g.review.action",
      "PUT /v1/marketplace/courses/:id/review",
      ["stars", "text"],
    ),
  ],
};

/** Report a course, review or learner. Reasons are the closed list the API validates. */
export const reportContent: GuidedScript = {
  id: "report.content",
  version: 1,
  entryPoints: ["marketplace"],
  flag: "FEATURE_GUIDED_REST",
  steps: [
    {
      id: "reason",
      widget: "single",
      promptKey: "g.report.q",
      options: opts("g.report.r", [
        "unsafe",
        "inappropriate",
        "spam",
        "copyright",
        "misleading",
        "other",
      ]),
      validate: { type: "enum" },
      persist: false,
      analyticsId: "reason",
    },
    {
      id: "details",
      widget: "longtext",
      promptKey: "g.report.details.q",
      validate: { type: "string", max: 500 },
      limit: "report.text",
      persist: false,
      skippable: true,
      analyticsId: "details",
    },
    confirm(
      "g.report.confirm.q",
      "g.report.action",
      "POST /v1/marketplace/reports",
      ["reason"],
    ),
  ],
};

/** Quote consent. The price lines stay on the panel; only an explicit confirm accepts. */
export const creditsConfirm: GuidedScript = {
  id: "credits.confirm",
  version: 1,
  entryPoints: ["quote"],
  flag: "FEATURE_GUIDED_REST",
  steps: [
    confirm(
      "g.credits.confirm.q",
      "g.credits.action",
      "POST /v1/quotes/:id/accept",
    ),
  ],
};

export const shareScripts = [
  courseShare,
  courseInvite,
  courseRepublish,
  courseReview,
  reportContent,
  creditsConfirm,
];
