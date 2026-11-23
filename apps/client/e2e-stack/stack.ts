// Full-stack flow harness: real web client, real API, real engine. No page.route mocks.
import { execFile } from "node:child_process";
import { mkdirSync, rmSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";
import {
  test as base,
  expect,
  type APIRequestContext,
  type Page,
} from "@playwright/test";

import { waitForCode } from "./codes";

const run = promisify(execFile);
const here = path.dirname(fileURLToPath(import.meta.url));
export const repoRoot = path.resolve(here, "../../..");

/** Browser origin of the stack under test (local compose stack or a remote URL). */
export const stackURL = (
  process.env.E2E_STACK_URL ??
  `http://localhost:${process.env.E2E_STACK_PORT ?? 18471}`
).replace(/\/$/, "");
/** Where per-flow screenshots and flows.json land. */
export const shotsDir = path.resolve(
  process.env.E2E_STACK_SHOTS_DIR ??
    path.join(here, "..", "test-results", "stack-flows"),
);
export const live =
  process.env.E2E_LIVE === "1" || process.env.E2E_LIVE === "true";

const codesSeen = new Map<string, number>();

/**
 * The sign-in code sent after the previous call for `email` (each call waits for a new
 * one). Locally codes come from the API's development log mailer via infra/e2e/stack.sh.
 * For a remote stack set E2E_STACK_CODE_COMMAND to a shell command that prints every code
 * sent to $1, one per line, oldest first.
 */
export async function signInCode(
  email: string,
  { reuse = false }: { reuse?: boolean } = {},
): Promise<string> {
  const custom = process.env.E2E_STACK_CODE_COMMAND;
  const seen = codesSeen.get(email) ?? 0;
  const { code, reused, count } = await waitForCode({
    seen,
    reuse,
    // A local log mailer is instant; real mail needs the resend cooldown plus delivery.
    timeoutMs:
      Number(process.env.E2E_STACK_CODE_TIMEOUT_MS) ||
      (custom ? 90_000 : 20_000),
    intervalMs: custom ? 1_000 : 500,
    list: async () => {
      const { stdout } = custom
        ? await run("sh", ["-c", custom, "sh", email])
        : await run("bash", [
            path.join(repoRoot, "infra/e2e/stack.sh"),
            "code",
            email,
          ]);
      return stdout.split("\n").filter((line) => /^\d{6}$/.test(line));
    },
  }).catch((error) => {
    throw new Error(`${String(error.message)} (${email})`);
  });
  if (!reused) codesSeen.set(email, count);
  return code;
}

export type Features = { lesson_test_tracer_enabled?: boolean } & Record<
  string,
  unknown
>;

export async function features(request: APIRequestContext): Promise<Features> {
  const response = await request.get(`${stackURL}/v1/features`);
  expect(response.ok(), "GET /v1/features").toBeTruthy();
  return (await response.json()) as Features;
}

export function uniqueEmail(flow: string): string {
  const id = `${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`;
  return `e2e-${flow.toLowerCase()}-${id}@${process.env.E2E_STACK_EMAIL_DOMAIN ?? "example.test"}`;
}

const slug = (text: string) =>
  text
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-|-$/g, "")
    .slice(0, 48);

export type Flow = {
  id: string;
  /** Full-page screenshot saved as <flow-id>/<nn>-<step>.png and attached to the test. */
  step: (name: string, page?: Page) => Promise<void>;
  /**
   * Record a product defect seen on the way that does not stop the flow's goal. It is
   * listed under `defects` in flows.json for the reviewer; the flow still passes.
   */
  defect: (description: string) => void;
};

type FlowOptions = { flowId: string };

/**
 * `test` for flow specs. Declare the flow with
 * `test.use({ flowId: "F1" })` and a `{ annotation: { type: "flow", description: "F1 Title" } }`.
 */
export const test = base.extend<FlowOptions & { flow: Flow }>({
  flowId: ["", { option: true }],
  flow: async ({ page, flowId }, provide, testInfo) => {
    if (!flowId) throw new Error("set test.use({ flowId }) in the flow spec");
    const dir = path.join(shotsDir, flowId);
    rmSync(dir, { recursive: true, force: true });
    mkdirSync(dir, { recursive: true });
    let n = 0;
    // The stack serves the web client through Caddy with the production Content-Security-Policy, so every
    // flow doubles as a policy check: a blocked script, frame, image or request fails the flow.
    const violations: string[] = [];
    page.on("console", (message) => {
      const text = message.text();
      if (
        /Content Security Policy|Refused to (execute|load|connect|apply|frame|create)/i.test(
          text,
        ) &&
        // Cloudflare injects its analytics beacon at the edge on a proxied zone; the policy blocks it by design.
        !/static\.cloudflareinsights\.com/.test(text) &&
        // A slide that nests its own <meta> CSP in <body>: ignored by the browser, not a violation.
        !/delivered via a <meta> element outside the document's <head>/.test(
          text,
        )
      )
        violations.push(text);
    });
    await provide({
      id: flowId,
      defect: (description) =>
        testInfo.annotations.push({ type: "defect", description }),
      step: async (name, target = page) => {
        n += 1;
        const file = `${String(n).padStart(2, "0")}-${slug(name)}.png`;
        const full = path.join(dir, file);
        await target.screenshot({ path: full, fullPage: true });
        await testInfo.attach(`flow-step:${file}:${name}`, {
          path: full,
          contentType: "image/png",
        });
      },
    });
    expect(violations, "Content-Security-Policy violations").toEqual([]);
  },
});
export { expect };

/** True when the suite targets the local infra/e2e stack (not a remote URL). */
export const localStack = /^http:\/\/(localhost|127\.0\.0\.1)(:\d+)?$/.test(
  stackURL,
);

/**
 * Record a purchase of `amount` credits. Card payments are disabled outside test builds
 * (STRIPE_MODE), so a real learner has no way to buy credits; this stands in for a
 * completed purchase. Locally it uses infra/e2e/stack.sh; for a remote stack set
 * E2E_STACK_GRANT_COMMAND (receives learner id as $1 and amount as $2). Returns false
 * when no grant mechanism exists, so the caller can mark the flow fixme.
 */
export async function grantCredits(
  learnerId: string,
  amount: number,
): Promise<boolean> {
  const custom = process.env.E2E_STACK_GRANT_COMMAND;
  if (custom) {
    await run("sh", ["-c", custom, "sh", learnerId, String(amount)]);
    return true;
  }
  if (!localStack) return false;
  await run("bash", [
    path.join(repoRoot, "infra/e2e/stack.sh"),
    "grant-credits",
    learnerId,
    String(amount),
  ]);
  return true;
}

/**
 * First-run welcome and tour (openspec first-run-onboarding-tour): a fresh learner who signs
 * up without a deep link lands on Learn behind the welcome, then the tour. Both block clicks
 * by design; this dismisses them the way a learner would ("Skip for now", then Escape).
 */
export async function skipOnboarding(page: Page): Promise<void> {
  await expect(page.getByTestId("onboarding-welcome")).toBeVisible();
  await page.getByRole("button", { name: "Skip for now", exact: true }).click();
  const tour = page.getByTestId("onboarding-tour");
  await expect(tour).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(tour).toHaveCount(0);
}

/**
 * Email-code sign-up through the real UI; ends signed in. Returns the learner id. Without
 * `returnTo` the learner lands on Learn and the first-run welcome and tour are skipped
 * unless `keepOnboarding` is set (F14 goes through them).
 */
export async function signUp(
  page: Page,
  email: string,
  flow?: Flow,
  returnTo?: string,
  { keepOnboarding = false }: { keepOnboarding?: boolean } = {},
): Promise<string> {
  // Observe (not intercept) the first /v1/me the app makes after verifying the code.
  const me = page.waitForResponse(
    (r) => new URL(r.url()).pathname === "/v1/me" && r.ok(),
    { timeout: 60_000 },
  );
  await page.goto(
    returnTo ? `/sign-in?returnTo=${encodeURIComponent(returnTo)}` : "/sign-in",
  );
  await expect(
    page.getByRole("heading", { name: "Learn any topic, a little at a time." }),
  ).toBeVisible();
  await flow?.step("welcome");
  await page.getByRole("button", { name: "Get started" }).click();
  await page.getByLabel("Email address").fill(email);
  await page.getByLabel("Email address").press("Enter");
  await page.getByLabel("I am 16 or older").check();
  await flow?.step("email entered");
  await page.getByRole("button", { name: "Send code" }).click();
  await expect(page.getByLabel("One-time code")).toBeVisible();
  await flow?.step("code requested");
  await page.getByLabel("One-time code").fill(await signInCode(email));
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(page).not.toHaveURL(/\/sign-in/, { timeout: 15_000 });
  const { id } = (await (await me).json()) as { id: string };
  if (!returnTo && !keepOnboarding) await skipOnboarding(page);
  return id;
}

/** Known app-shell defects worth a reviewer's attention; recorded, never fatal. */
export async function noteShellDefects(page: Page, flow: Flow): Promise<void> {
  // Panels load independently; SSE streams may keep the network busy, so cap the wait.
  await page
    .waitForLoadState("networkidle", { timeout: 5_000 })
    .catch(() => {});
  if (await page.getByText("Could not load this course.").isVisible())
    flow.defect(
      "Learn home for a learner with no courses shows 'Could not load this course.': the shell " +
        "defaults the active course to the fixture id 'astronomy' " +
        "(apps/client/src/features/shell/ShellSession.tsx:14) so IndexRoute renders LearnPath " +
        "instead of the empty Courses state (apps/client/src/App.tsx:125).",
    );
  if (await page.getByText("Code is invalid or expired.").isVisible())
    flow.defect(
      "Daily goal panel shows the sign-in error 'Code is invalid or expired.': DailyGoal calls " +
        "authRequest('/v1/gamification') without the access token " +
        "(apps/client/src/features/gamification/DailyGoal.tsx:31,54), so the API answers 401.",
    );
}
