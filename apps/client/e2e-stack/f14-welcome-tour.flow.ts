import { test } from "./stack";

test.use({ flowId: "F14" });

// Slots in when the first-run welcome and guided tour (PR #311) is on main: sign up, go
// THROUGH the welcome and every tour step with a screenshot of each (owner review), and
// make the other flows pre-complete onboarding through the production-safe mechanism that
// PR documents.
test.fixme(
  "F14 first-run welcome and guided tour, every step",
  {
    annotation: [
      { type: "flow", description: "F14 First-run welcome and guided tour" },
      {
        type: "fixme",
        description:
          "waiting on PR #311 (first-run welcome and guided tour), not merged into main yet",
      },
    ],
  },
  async () => {},
);
