import "./messages.social";
import type { GuidedScript } from "./types";

/** Claim or change the public username. Shape is checked per step, uniqueness on submit. */
export const socialUsername: GuidedScript = {
  id: "social.username",
  version: 1,
  entryPoints: ["settings"],
  flag: "FEATURE_GUIDED_REST",
  steps: [
    {
      id: "username",
      widget: "text",
      promptKey: "g.username.q",
      validate: { type: "string", min: 3, max: 20 },
      persist: false,
      input: {
        name: "username",
        autocomplete: "off",
        labelKey: "g.username.label",
      },
      analyticsId: "username",
    },
    {
      id: "confirm",
      widget: "confirm",
      promptKey: "g.username.confirm.q",
      summaryOf: ["username"],
      actionKey: "g.username.action",
      persist: false,
      submit: "PUT /v1/social/username",
      analyticsId: "confirm",
    },
  ],
};

/** Add a friend by exact username. Lookup is neutral: a miss never says why. */
export const socialAddFriend: GuidedScript = {
  id: "social.add_friend",
  version: 1,
  entryPoints: ["friends"],
  flag: "FEATURE_GUIDED_REST",
  steps: [
    {
      id: "username",
      widget: "text",
      promptKey: "g.friend.q",
      validate: { type: "string", min: 3, max: 32 },
      persist: false,
      input: {
        name: "friend-username",
        autocomplete: "off",
        labelKey: "g.friend.label",
      },
      analyticsId: "username",
    },
    {
      id: "confirm",
      widget: "confirm",
      promptKey: "g.friend.confirm.q",
      summaryOf: ["username"],
      actionKey: "g.friend.action",
      persist: false,
      submit: "POST /v1/social/friend-requests",
      analyticsId: "confirm",
    },
  ],
};
