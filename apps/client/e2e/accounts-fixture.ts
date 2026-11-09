import { apiBase, resetAuthRateLimits } from "./servers";
import { test as base, expect } from "@playwright/test";
import type { Tokens } from "../src/features/accounts/secure-store";
let tokens: Tokens | undefined;
export function resetTokens() {
  tokens = undefined;
}
export const test = base.extend<{ authenticated: void }>({
  authenticated: [
    async ({ context, request, page }, use) => {
      await resetAuthRateLimits(request);
      if (!tokens) {
        const email = `browser-${Date.now()}@example.test`;
        expect(
          (
            await request.post(apiBase + "/v1/auth/code", {
              data: { email, age_confirmed: true },
            })
          ).status(),
        ).toBe(202);
        const { code } = await (
          await request.get(`${apiBase}/test/mail?email=${email}`)
        ).json();
        const response = await request.post(apiBase + "/v1/auth/verify", {
          data: { email, code, age_confirmed: true },
        });
        expect(response.status()).toBe(200);
        tokens = (await response.json()) as Tokens;
      }
      await context.addInitScript((value) => {
        sessionStorage.setItem("duolify.auth", JSON.stringify(value));
      }, tokens);
      await page.route(/\/v1\/(auth\/|me(?:\/|$|\?))/, async (route) => {
        const original = route.request();
        const target = new URL(original.url());
        const headers = original.headers();
        delete headers.origin;
        target.host = new URL(apiBase).host;
        await route.fulfill({
          response: await request.fetch(target.href, {
            method: original.method(),
            headers,
            data: original.postData() ?? undefined,
          }),
        });
      });
      await use();
    },
    { auto: true },
  ],
});
export { expect };
