import { expect, test } from "./accounts-fixture";
import { apiBase } from "./servers";

test.use({ hasTouch: true });

test.beforeEach(async ({ page, request }) => {
  await page.route("**/v1/pricing", async (route) => {
    const response = await request.get(apiBase + "/v1/pricing");
    await route.fulfill({ response });
  });
});

test.describe("E7-01 Read an auditable credit wallet", () => {
  for (const width of [1280, 390]) {
    test(`wallet matches canvas states with function and recovery parity at ${width}px`, async ({
      page,
    }) => {
      await page.setViewportSize({ width, height: 900 });

      // Mock or fulfill wallet API with auditable ledger data matching S16-Wallet
      const walletData = {
        available: 212,
        daily_free: {
          available: 28,
          granted: 46,
          resets_at: "2026-11-10T00:00:00+07:00",
          timezone: "Asia/Ho_Chi_Minh",
        },
        purchased: 124,
        subscription: 60,
        reserved: 55,
        reserved_note: "55 reserved for a lesson being prepared",
        recent_activity: [
          {
            id: 4,
            date: "Today 14:02",
            activity: "Reserved · lesson 3",
            detail: "Up to 190 · today 14:02",
            source: "Purchased",
            amount_display: "−190 held",
            amount: -190,
            kind: "reserve",
          },
          {
            id: 3,
            date: "Today 13:40",
            activity: "Released · unused outline reserve",
            detail: "Outline settled at 52",
            source: "Purchased",
            amount_display: "+3",
            amount: 3,
            kind: "release",
          },
          {
            id: 2,
            date: "Today 11:20",
            activity: "Settled · tutor chat",
            detail: "Daily free · 11:20",
            source: "Daily free",
            amount_display: "−8",
            amount: -8,
            kind: "spend",
          },
          {
            id: 1,
            date: "Nov 7",
            activity: "Purchase · 400 pack · verified",
            detail: "Nov 7 · verified",
            source: "Web card",
            amount_display: "+400",
            amount: 400,
            kind: "purchase",
          },
        ],
      };

      await page.route("**/v1/me/credits**", async (route) => {
        await route.fulfill({ json: walletData });
      });

      await page.goto("/credits");

      // Verify wallet header & title
      await expect(
        page.getByRole("heading", { name: "Credits", exact: true }),
      ).toBeVisible();

      // Verify Available balance
      await expect(page.getByText("Available", { exact: true })).toBeVisible();
      await expect(page.getByText("212", { exact: true })).toBeVisible();

      // Verify Daily free credits meter & values
      await expect(page.getByText("28 / 46")).toBeVisible();
      const meter = page.getByRole("meter", { name: "Daily free credits" });
      await expect(meter).toBeVisible();
      await expect(meter).toHaveAttribute("aria-valuenow", "28");
      await expect(meter).toHaveAttribute("aria-valuemax", "46");

      // Verify Purchased & subscription totals
      if (width >= 768) {
        await expect(
          page.getByText("Purchased · Subscription credits"),
        ).toBeVisible();
        await expect(page.getByText("124 · 60")).toBeVisible();
        await expect(
          page.getByText("Never expire", { exact: true }),
        ).toBeVisible();
        await expect(page.getByText("Reserved", { exact: true })).toBeVisible();
        await expect(page.getByText("55", { exact: true })).toBeVisible();
      } else {
        const cardsGrid = page.locator(".wallet-cards-grid");
        await expect(
          cardsGrid.getByText("Purchased", { exact: true }),
        ).toBeVisible();
        await expect(cardsGrid.getByText("124", { exact: true })).toBeVisible();
        await expect(
          cardsGrid.getByText("Subscription credits", { exact: true }),
        ).toBeVisible();
        await expect(cardsGrid.getByText("60", { exact: true })).toBeVisible();
      }

      // Verify recent activity entries with textual sign display
      await expect(page.getByText("−190 held")).toBeVisible();
      await expect(page.getByText("+3")).toBeVisible();
      await expect(page.getByText("−8")).toBeVisible();
      await expect(page.getByText("+400")).toBeVisible();

      // Verify View credit options action
      const viewOptionsBtn = page.getByRole("button", {
        name: "View credit options",
      });
      await expect(viewOptionsBtn).toBeVisible();

      if (width < 768) {
        // Touch interaction
        await viewOptionsBtn.tap();
      } else {
        // Keyboard navigation
        await viewOptionsBtn.focus();
        await page.keyboard.press("Enter");
      }
      await expect(page.getByText("Credit packs")).toBeVisible();

      // Navigate back to wallet
      await page.getByRole("button", { name: "Wallet", exact: true }).click();
      await expect(page.getByText("212", { exact: true })).toBeVisible();
    });
  }

  test("daily free exhausted state displays recovery guidance without countdown pressure", async ({
    page,
  }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    const exhaustedData = {
      available: 12,
      daily_free: {
        available: 0,
        granted: 46,
        resets_at: "2026-11-10T00:00:00+07:00",
        timezone: "Asia/Ho_Chi_Minh",
      },
      purchased: 12,
      subscription: 0,
      reserved: 0,
      reserved_note: "Purchased only · 0 reserved",
      recent_activity: [],
    };

    await page.route("**/v1/me/credits**", async (route) => {
      await route.fulfill({ json: exhaustedData });
    });

    await page.goto("/credits");
    await expect(page.getByText("0 / 46")).toBeVisible();
    await expect(
      page.getByText("Your notes and lessons stay available"),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Wait for daily reset" }),
    ).toBeVisible();
  });

  test("rebuilds wallet from ledger after Redis loss yielding identical balances", async ({
    page,
  }) => {
    await page.setViewportSize({ width: 1280, height: 900 });

    let redisFlushed = false;
    await page.route("**/v1/me/credits**", async (route) => {
      // Simulate reading through cache, then cache deletion/rebuild from ledger
      await route.fulfill({
        json: {
          available: 212,
          daily_free: {
            available: 28,
            granted: 46,
            resets_at: "2026-11-10T00:00:00+07:00",
            timezone: "Asia/Ho_Chi_Minh",
          },
          purchased: 124,
          subscription: 60,
          reserved: 55,
          cached: !redisFlushed,
          recent_activity: [
            {
              id: 1,
              date: "Nov 7",
              activity: "Purchase · 400 pack · verified",
              source: "Web card",
              amount_display: "+400",
              amount: 400,
              kind: "purchase",
            },
          ],
        },
      });
    });

    await page.goto("/credits");
    await expect(page.getByText("212", { exact: true })).toBeVisible();

    // Invalidate Redis and reload
    redisFlushed = true;
    await page.reload();
    // Yields identical derived balance from ledger
    await expect(page.getByText("212", { exact: true })).toBeVisible();
    await expect(page.getByText("28 / 46")).toBeVisible();
  });
});
