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
      // Demo route aliases belong only to the disposable browser fixture. Real
      // UUID routes exercise the same API authorization and object reader.
      let demoLesson: string | undefined;
      await page.route("**/v1/lessons/**", async (route) => {
        const original = route.request();
        const target = new URL(original.url());
        const headers = original.headers();
        delete headers.origin;
        target.host = new URL(apiBase).host;
        const demo = target.pathname.startsWith("/v1/lessons/sky/");
        if (demo) {
          if (!demoLesson) {
            const response = await request.post(
              `${apiBase}/test/player/fixture`,
              { headers: { Authorization: headers.authorization } },
            );
            if (!response.ok())
              throw new Error(`Player fixture failed: ${response.status()}`);
            demoLesson = (await response.json()).lesson_id;
          }
          target.pathname = target.pathname.replace(
            "/lessons/sky/",
            `/lessons/${demoLesson}/`,
          );
        }
        try {
          const response = await request.fetch(target.href, {
            method: original.method(),
            headers,
            data: original.postData() ?? undefined,
          });
          if (demo && target.pathname.endsWith("/manifest") && response.ok()) {
            const manifest = await response.json();
            manifest.lesson_id = "sky";
            manifest.course_id = "astronomy";
            for (const slide of manifest.slides)
              slide.html_url = slide.html_url.replace(
                `/lessons/${demoLesson}/`,
                "/lessons/sky/",
              );
            await route.fulfill({ json: manifest });
          } else await route.fulfill({ response });
        } catch {
          // Request aborted or response disposed during navigation
        }
      });
      await use();
    },
    { auto: true },
  ],
});
export { expect };
