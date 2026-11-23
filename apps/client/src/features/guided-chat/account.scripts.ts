import "./messages.account";
import type { GuidedScript } from "./types";

/** Encrypted data export. The passphrase is typed in a real password field and never stored. */
export const accountExport: GuidedScript = {
  id: "account.export",
  version: 1,
  entryPoints: ["settings"],
  flag: "FEATURE_GUIDED_REST",
  steps: [
    {
      id: "passphrase",
      widget: "password",
      promptKey: "g.export.pass.q",
      validate: { type: "string", min: 12, max: 256 },
      persist: false,
      input: {
        name: "export-passphrase",
        autocomplete: "new-password",
        labelKey: "g.export.pass.label",
      },
      analyticsId: "passphrase",
    },
    {
      id: "confirm",
      widget: "confirm",
      promptKey: "g.export.confirm.q",
      summaryOf: [],
      actionKey: "g.export.action",
      persist: false,
      submit: "GET /v1/me/export",
      analyticsId: "confirm",
    },
  ],
};

/** Account deletion. Only the destructive card's button confirms; free text never can. */
export const accountDelete: GuidedScript = {
  id: "account.delete",
  version: 1,
  entryPoints: ["settings"],
  flag: "FEATURE_GUIDED_REST",
  steps: [
    {
      id: "confirm",
      widget: "confirm",
      promptKey: "g.delete.q",
      summaryOf: [],
      destructive: true,
      actionKey: "g.delete.action",
      persist: false,
      submit: "DELETE /v1/me",
      analyticsId: "confirm",
    },
  ],
};
