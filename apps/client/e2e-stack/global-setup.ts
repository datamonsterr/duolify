// Runs once before the flow suite. On the local infra/e2e stack only, it makes sure the stack's
// active pricing version quotes the route the engine serves, by having the stack's own test owner
// drive the unmodified pricing admin flow (infra/e2e/stack.sh approve-pricing). Against any other
// target it never attempts approval: flows that need a quote report "pricing not approved on
// target" if the target's own pricing is not approved.
import { execFile } from "node:child_process";
import path from "node:path";
import { promisify } from "node:util";
import { localStack, repoRoot } from "./stack";

const run = promisify(execFile);

export default async function globalSetup(): Promise<void> {
  if (!localStack || process.env.E2E_STACK_SKIP_PRICING === "1") return;
  const { stderr } = await run(
    "bash",
    [path.join(repoRoot, "infra/e2e/stack.sh"), "approve-pricing"],
    { timeout: 180_000 },
  );
  process.stderr.write(stderr);
}
