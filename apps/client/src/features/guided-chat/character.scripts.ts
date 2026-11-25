import "./messages.character";
import type { GuidedScript, Step } from "./types";

const selectorStep = (id: string, tab: "kind" | "look" | "outfit"): Step => ({
  id,
  widget: "character",
  promptKey: `g.char.${id}.q`,
  character: { tab },
  actionKey: "g.char.step.action",
  initial: "done",
  validate: { type: "string", max: 8 },
  persist: false,
  analyticsId: id,
});

/**
 * Create a Character from selectors, as a chat. Each selector step is the same
 * `CharacterSelector` the editor uses, patching one draft document. No Credits are quoted
 * or spent; the prompt path and its screening are unchanged and live elsewhere.
 */
export const characterCreate: GuidedScript = {
  id: "character.create",
  version: 1,
  entryPoints: ["characters"],
  flag: "FEATURE_GUIDED_CHARACTER",
  steps: [
    selectorStep("kind", "kind"),
    selectorStep("look", "look"),
    selectorStep("outfit", "outfit"),
    {
      id: "name",
      widget: "text",
      promptKey: "g.char.name.q",
      validate: { type: "string", min: 1, max: 40 },
      persist: false,
      input: {
        name: "character-name",
        autocomplete: "off",
        labelKey: "g.char.name.label",
      },
      analyticsId: "name",
    },
    {
      id: "pronouns",
      widget: "single",
      promptKey: "g.char.pronouns.q",
      options: ["she", "he", "they", "none"].map((id) => ({
        id,
        labelKey: `g.char.pronouns.${id}`,
      })),
      validate: { type: "enum" },
      persist: false,
      analyticsId: "pronouns",
    },
    {
      id: "confirm",
      widget: "confirm",
      promptKey: "g.char.confirm.q",
      summaryOf: ["name", "pronouns"],
      actionKey: "g.char.action",
      persist: false,
      submit: "POST /v1/characters",
      analyticsId: "confirm",
    },
  ],
};

const pick = (
  id: string,
  pickOf: "guide" | "support",
  rest: Partial<Step> = {},
): Step => ({
  id,
  widget: "character",
  promptKey: `g.cast.${id}.q`,
  character: { pick: pickOf },
  actionKey: "g.cast.pick",
  validate: { type: "string", max: 80 },
  persist: false,
  analyticsId: id,
  ...rest,
});

/** Choose a guide and up to two supporting Characters for a Course. Saving costs nothing. */
export const characterCast: GuidedScript = {
  id: "character.cast",
  version: 1,
  entryPoints: ["course_cast"],
  flag: "FEATURE_GUIDED_CHARACTER",
  steps: [
    pick("guide", "guide", {
      next: [{ when: { eq: ["guide", "default"] }, to: "confirm" }],
    }),
    pick("support1", "support", {
      next: [{ when: { eq: ["support1", "none"] }, to: "confirm" }],
    }),
    pick("support2", "support"),
    {
      id: "confirm",
      widget: "confirm",
      promptKey: "g.cast.confirm.q",
      summaryOf: ["guide", "support1", "support2"],
      actionKey: "g.cast.action",
      persist: false,
      submit: "PUT /v1/courses/{id}/cast",
      analyticsId: "confirm",
    },
  ],
};
