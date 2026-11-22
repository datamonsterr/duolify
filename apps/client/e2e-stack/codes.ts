// Waiting for a sign-in code, separated from Playwright so it is unit-testable.

/** The API's resend cooldown: within it an unused code is not mailed again (never weakened). */
export const RESEND_COOLDOWN_MS = 60_000;

export type WaitForCodeOptions = {
  /** Every code mailed to the address so far, oldest first. */
  list: () => Promise<string[]>;
  /** How many codes the caller has already consumed. */
  seen: number;
  /**
   * The caller requested a code while an earlier unused code was still valid. The API then
   * mails nothing and the existing code stays valid, so it is accepted once the wait for a
   * fresh mail ends.
   */
  reuse?: boolean;
  /** Real mail needs more than a log line: cooldown plus delivery. */
  timeoutMs?: number;
  intervalMs?: number;
  now?: () => number;
  sleep?: (ms: number) => Promise<void>;
};

export async function waitForCode(
  options: WaitForCodeOptions,
): Promise<{ code: string; reused: boolean; count: number }> {
  const {
    list,
    seen,
    reuse = false,
    timeoutMs = 90_000,
    intervalMs = 1_000,
    now = Date.now,
    sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms)),
  } = options;
  const deadline = now() + timeoutMs;
  let last = "no attempt";
  let latest: string[] = [];
  while (now() < deadline) {
    try {
      latest = await list();
      if (latest.length > seen)
        return {
          code: latest[latest.length - 1],
          reused: false,
          count: latest.length,
        };
      last = `${latest.length} code(s), waiting for code ${seen + 1}`;
    } catch (error) {
      last = String(error).split("\n")[0];
    }
    await sleep(intervalMs);
  }
  if (reuse && seen > 0 && latest.length >= seen)
    return { code: latest[seen - 1], reused: true, count: seen };
  throw new Error(
    `no sign-in code after ${Math.round(timeoutMs / 1000)}s: ${last}. ` +
      `The API does not mail a new code while an unused one was sent within ${RESEND_COOLDOWN_MS / 1000}s ` +
      `(the existing code stays valid); otherwise check mail delivery to the test inbox.`,
  );
}
