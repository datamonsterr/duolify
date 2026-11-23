import type { GuidedScript } from "./types";

/** Email code sign-in. Pre-auth: nothing is drafted or stored, answers live in memory only. */
export const authSignin: GuidedScript = {
  id: "auth.signin",
  version: 1,
  entryPoints: ["sign_in"],
  flag: "FEATURE_GUIDED_SIGNIN",
  steps: [
    {
      id: "email",
      widget: "email",
      promptKey: "g.signin.email.q",
      validate: { type: "email" },
      persist: false,
      input: { name: "email", autocomplete: "email", inputMode: "email" },
      analyticsId: "email",
    },
    {
      id: "age",
      widget: "confirm",
      promptKey: "g.signin.age.q",
      summaryOf: [],
      checks: ["g.signin.age.check"],
      actionKey: "g.signin.send_code",
      persist: false,
      analyticsId: "age",
    },
    {
      id: "code",
      widget: "code",
      promptKey: "g.signin.code.q",
      validate: { type: "code", length: 6 },
      limit: "auth.code",
      persist: false,
      input: {
        name: "code",
        autocomplete: "one-time-code",
        inputMode: "numeric",
      },
      actionKey: "g.signin.sign_in",
      submit: "POST /v1/auth/verify",
      analyticsId: "code",
    },
  ],
};
