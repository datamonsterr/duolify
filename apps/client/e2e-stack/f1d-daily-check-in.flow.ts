import { test } from "./stack";

test.use({ flowId: "F1d" });

// Slots in when daily check-in credits (PR #300) are on main: the first open of the day
// shows the check-in toast (250 Credits on the first check-in, 150 per day after) through
// POST /v1/me/check-in. Course flows then fund learners through the real check-in (course
// creation 80 + first lesson 140 fits the first one) and keep grant-credits only where a
// flow needs more; F1b adds the toast to its screenshots.
test.fixme(
  "F1d daily check-in credits on the first open of the day",
  {
    annotation: [
      { type: "flow", description: "F1d Daily check-in credits toast" },
      {
        type: "fixme",
        description:
          "waiting on PR #300 (daily check-in credits), not merged into main yet",
      },
    ],
  },
  async () => {},
);
