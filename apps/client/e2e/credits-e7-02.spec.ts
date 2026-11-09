import { expect, test } from "@playwright/test";
import { apiBase, resetAuthRateLimits, webBase } from "./servers";
import type { Tokens } from "../src/features/accounts/secure-store";

test.use({ hasTouch: true });

// These scenarios use the real authenticated API and Postgres. Browser requests
// reach it through Vite's fixture proxy; no Playwright route supplies responses.
for (const width of [1280, 390]) {
  for (const expired of [false, true]) {
    test(`E7 ${expired ? "expired yesterday" : "fresh"} grant is idempotent and timezone changes are delayed at ${width}px`, async ({
      page,
      context,
      request,
    }) => {
      await resetAuthRateLimits(request);
      const email = `daily-${width}-${Date.now()}@example.test`;
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
      const login = await request.post(apiBase + "/v1/auth/verify", {
        data: {
          email,
          code,
          age_confirmed: true,
          time_zone: "Asia/Ho_Chi_Minh",
        },
      });
      expect(login.status()).toBe(200);
      const tokens = (await login.json()) as Tokens;
      const headers = { Authorization: `Bearer ${tokens.access_token}` };
      if (expired) {
        expect(
          (
            await request.post(apiBase + "/test/credits/yesterday", { headers })
          ).status(),
        ).toBe(204);
      }
      await context.addInitScript((value) => {
        sessionStorage.setItem("duolify.auth", JSON.stringify(value));
      }, tokens);
      await page.setViewportSize({ width, height: 900 });
      const wallets = await Promise.all(
        Array.from({ length: 8 }, () =>
          request.get(apiBase + "/v1/me/credits", { headers }),
        ),
      );
      for (const response of wallets) expect(response.status()).toBe(200);
      const initial = await wallets[0].json();
      expect(initial.daily_free.available).toBe(initial.daily_free.granted);
      expect(initial.daily_free.granted).toBeGreaterThan(0);
      await page.goto(webBase + "/credits");
      const meter = page.getByRole("meter", { name: "Daily free credits" });
      await expect(meter).toHaveAttribute(
        "aria-valuenow",
        String(initial.daily_free.available),
      );
      await expect(meter).toHaveAttribute(
        "aria-valuemax",
        String(initial.daily_free.granted),
      );
      const exported = await request.get(apiBase + "/v1/me/data-export", {
        headers,
      });
      expect(exported.status()).toBe(200);
      const data = await exported.json();
      expect(
        data.tables.credit_entries.filter(
          (row: { kind: string }) => row.kind === "daily_grant",
        ),
      ).toHaveLength(expired ? 2 : 1);
      if (expired) {
        const expirations = data.tables.credit_entries.filter(
          (row: { kind: string }) => row.kind === "expire",
        );
        expect(expirations).toHaveLength(1);
        expect(expirations[0].amount).toBe(-initial.daily_free.granted);
        await expect(page.getByText("Expired · daily free")).toBeVisible();
      }
      for (const endpoint of ["profile", "settings"]) {
        const saved = await request.post(`${apiBase}/v1/me/${endpoint}`, {
          headers,
          data: {
            confirmed: true,
            ...(endpoint === "profile" ? { action: "save" } : {}),
            fields: { time_zone: "America/New_York" },
          },
        });
        expect(saved.status()).toBe(200);
        const response = await request.get(apiBase + "/v1/me/credits", {
          headers,
        });
        expect(response.status()).toBe(200);
        const wallet = await response.json();
        expect(wallet.daily_free.timezone).toBe("Asia/Ho_Chi_Minh");
        expect(wallet.daily_free.resets_at).toBe(initial.daily_free.resets_at);
        await page.reload();
        await expect(
          page.getByText("Resets 00:00 Ho Chi Minh City"),
        ).toBeVisible();
      }
    });
  }
}
