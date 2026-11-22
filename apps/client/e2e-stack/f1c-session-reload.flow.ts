import { expect, signUp, test, uniqueEmail } from "./stack";

test.use({ flowId: "F1c" });

const reason =
  "Web credentials live only in memory (apps/client/src/features/accounts/secure-store.ts:16-35), " +
  "so any reload, typed URL or new tab signs the learner out. Persistent web sessions are " +
  "planned after the Clerk sign-in PR #276 merges.";

test(
  "F1c web session survives a page reload",
  {
    annotation: {
      type: "flow",
      description: "F1c Web session survives a page reload",
    },
  },
  async ({ page, flow }) => {
    test.fixme(true, reason);
    await signUp(page, uniqueEmail("f1c"));
    await page.reload();
    await expect(page).not.toHaveURL(/\/sign-in/);
    await flow.step("reloaded, still signed in");
  },
);
