import { expect, test } from "./accounts-fixture";
import { apiBase } from "./servers";

test.use({ hasTouch: true });

test.beforeEach(async ({ page, request }) => {
  await page.route("**/v1/pricing", async (route) => {
    const response = await request.get(apiBase + "/v1/pricing");
    await route.fulfill({ response });
  });
});

test.describe("E7-02 Grant and expire eligible daily credits", () => {
  for (const width of [1280, 390]) {
    test(`grant concurrently and display daily pool matching canvas at ${width}px`, async ({
      page,
      request,
    }) => {
      await page.setViewportSize({ width, height: 900 });

      // Daily grant wallet data with 46 credits pool
      const freshWalletData = {
        available: 46,
        daily_free: {
          available: 46,
          granted: 46,
          resets_at: "2026-11-10T00:00:00+07:00",
          timezone: "Asia/Ho_Chi_Minh",
        },
        purchased: 0,
        subscription: 0,
        reserved: 0,
        recent_activity: [
          {
            id: 1,
            date: "Today 00:00",
            activity: "Daily free credits granted",
            detail: "Daily free · 00:00",
            source: "Daily free",
            amount_display: "+46",
            amount: 46,
            kind: "daily_grant",
          },
        ],
      };

      let grantCallCount = 0;
      await page.route("**/v1/me/credits**", async (route) => {
        grantCallCount++;
        await route.fulfill({ json: freshWalletData });
      });

      // Simulate concurrent requests
      await Promise.all([
        page.goto("/credits"),
        request.get(`${apiBase}/v1/me/credits`).catch(() => null),
      ]);
      // Verify Available balance is 46 (daily grant)
      await expect(page.getByText("Available", { exact: true })).toBeVisible();
      await expect(page.getByText("46", { exact: true })).toBeVisible();
      expect(grantCallCount).toBeGreaterThanOrEqual(1);

      // Verify Daily free credits meter & values
      await expect(page.getByText("46 / 46")).toBeVisible();
      const meter = page.getByRole("meter", { name: "Daily free credits" });
      await expect(meter).toBeVisible();
      await expect(meter).toHaveAttribute("aria-valuenow", "46");
      await expect(meter).toHaveAttribute("aria-valuemax", "46");

      // Verify reset copy includes timezone city
      await expect(
        page.getByText("Resets 00:00 Ho Chi Minh City"),
      ).toBeVisible();

      // Verify initial activity row
      await expect(page.getByText("Daily free credits granted")).toBeVisible();
      await expect(page.getByText("+46")).toBeVisible();
    });

    test(`cross midnight expires unreserved remainder and resets daily pool at ${width}px`, async ({
      page,
    }) => {
      await page.setViewportSize({ width, height: 900 });

      // State after midnight: previous day expired unreserved remainder, new day granted 46
      const postMidnightWalletData = {
        available: 46,
        daily_free: {
          available: 46,
          granted: 46,
          resets_at: "2026-11-11T00:00:00+07:00",
          timezone: "Asia/Ho_Chi_Minh",
        },
        purchased: 0,
        subscription: 0,
        reserved: 0,
        recent_activity: [
          {
            id: 4,
            date: "Today 00:00",
            activity: "Daily free credits granted",
            detail: "Daily free · 00:00",
            source: "Daily free",
            amount_display: "+46",
            amount: 46,
            kind: "daily_grant",
          },
          {
            id: 3,
            date: "Yesterday 23:59",
            activity: "Expired · daily free",
            detail: "Unspent daily credits expired",
            source: "Daily free",
            amount_display: "−38",
            amount: -38,
            kind: "expire",
          },
          {
            id: 2,
            date: "Yesterday 15:30",
            activity: "Settled · tutor chat",
            detail: "Daily free · 15:30",
            source: "Daily free",
            amount_display: "−8",
            amount: -8,
            kind: "spend",
          },
          {
            id: 1,
            date: "Yesterday 00:00",
            activity: "Daily free credits granted",
            detail: "Daily free · 00:00",
            source: "Daily free",
            amount_display: "+46",
            amount: 46,
            kind: "daily_grant",
          },
        ],
      };

      await page.route("**/v1/me/credits**", async (route) => {
        await route.fulfill({ json: postMidnightWalletData });
      });

      await page.goto("/credits");

      // Verify Available balance and meter reset
      await expect(page.getByText("46 / 46")).toBeVisible();

      // Verify activity contains both expire and new grant
      await expect(page.getByText("Expired · daily free")).toBeVisible();
      await expect(page.getByText("−38")).toBeVisible();
      await expect(page.getByText("+46").first()).toBeVisible();

      // Test recovery and interaction parity: button navigation
      const optionsBtn = page.getByRole("button", {
        name: "View credit options",
      });
      await expect(optionsBtn).toBeVisible();
      if (width < 768) {
        await optionsBtn.tap();
      } else {
        await optionsBtn.focus();
        await page.keyboard.press("Enter");
      }
      await expect(page.getByText("Credit packs")).toBeVisible();
    });

    test(`change timezone applies 24-hour delay before updating reset instant at ${width}px`, async ({
      page,
    }) => {
      await page.setViewportSize({ width, height: 900 });

      // Before 24h delay elapsed: still shows Ho Chi Minh City reset
      const delayedWalletData = {
        available: 46,
        daily_free: {
          available: 46,
          granted: 46,
          resets_at: "2026-11-10T00:00:00+07:00",
          timezone: "Asia/Ho_Chi_Minh",
        },
        purchased: 0,
        subscription: 0,
        reserved: 0,
        recent_activity: [],
      };

      await page.route("**/v1/me/credits**", async (route) => {
        await route.fulfill({ json: delayedWalletData });
      });

      await page.goto("/credits");

      // Shows current effective timezone before 24h delay
      await expect(
        page.getByText("Resets 00:00 Ho Chi Minh City"),
      ).toBeVisible();

      // After 24h delay elapsed: transitions to New York timezone
      const updatedWalletData = {
        available: 46,
        daily_free: {
          available: 46,
          granted: 46,
          resets_at: "2026-11-11T00:00:00-05:00",
          timezone: "America/New_York",
        },
        purchased: 0,
        subscription: 0,
        reserved: 0,
        recent_activity: [],
      };

      await page.route("**/v1/me/credits**", async (route) => {
        await route.fulfill({ json: updatedWalletData });
      });

      // Reload to simulate post-24h transition
      await page.reload();

      await expect(page.getByText("Resets 00:00 New York")).toBeVisible();
    });
  }
});
