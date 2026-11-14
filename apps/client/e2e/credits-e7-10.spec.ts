import { test, expect, resetTokens } from "./accounts-fixture";
import { apiBase } from "./servers";

test.use({ hasTouch: true });
test.afterEach(() => resetTokens());

const widths = [1280, 390, 320];

async function setup(
  page: import("@playwright/test").Page,
  request: import("@playwright/test").APIRequestContext,
  width: number,
) {
  await page.setViewportSize({ width, height: 900 });
  await page.route(
    /\/v1\/(pricing$|quotes|credits\/(checkouts|subscription))/,
    async (route) => {
      const original = route.request();
      const headers = original.headers();
      delete headers.origin;
      headers["x-test-stripe-fixture"] = "true";
      await route.fulfill({
        response: await request.fetch(
          apiBase + new URL(original.url()).pathname,
          {
            method: original.method(),
            headers,
            data: original.postData() ?? undefined,
          },
        ),
      });
    },
  );
  await page.goto("/credits");
  const tokens = await page.evaluate(() =>
    JSON.parse(sessionStorage.getItem("duolify.native-store-fixture")!),
  );
  const authHeaders = { Authorization: `Bearer ${tokens.access_token}` };
  const purchased = async () =>
    (
      await (
        await request.get(`${apiBase}/v1/me/credits`, { headers: authHeaders })
      ).json()
    ).purchased as number;

  // Buys the 400-credit pack through the real UI and fulfils it; returns a notifier.
  const buyPack = async () => {
    await page.getByRole("button", { name: "Browse prices" }).click();
    await page.getByRole("radio", { name: /Try/ }).check();
    const buyButton = page.getByRole("button", { name: "Preview checkout" });
    if (width >= 768) {
      await buyButton.focus();
      await page.keyboard.press("Enter");
    } else {
      await buyButton.tap();
    }
    await expect(
      page.getByText("Test checkout — no real payment is taken"),
    ).toBeVisible();
    const orderId = await page.evaluate(
      () =>
        Object.entries(sessionStorage).find(([key]) =>
          key.endsWith(":order"),
        )?.[1],
    );
    expect(orderId).toBeTruthy();
    const notify = async (
      state: string,
      event: string,
      extra?: Record<string, unknown>,
    ) => {
      const response = await request.post(`${apiBase}/test/stripe/${orderId}`, {
        data: { state, event, ...extra },
      });
      expect(response.ok()).toBe(true);
    };
    await notify("paid", "checkout.session.completed");
    await page.reload();
    await expect(
      page.getByRole("heading", { name: "Credits added", exact: true }),
    ).toBeVisible();
    await page.getByRole("button", { name: "Return to saved work" }).click();
    return notify;
  };
  const spend = async (amount: number) => {
    const res = await request.post(apiBase + "/test/credits/spend-remaining", {
      headers: authHeaders,
      data: { amount },
    });
    expect(res.status()).toBe(200);
  };
  return { authHeaders, purchased, buyPack, spend };
}

async function expectNoPrivateDetails(page: import("@playwright/test").Page) {
  const text = await page.locator("body").innerText();
  expect(text).not.toMatch(/\b(re|dp|ch|pi|py|cs)_[A-Za-z0-9_]+/);
  expect(text).not.toMatch(/\*{4}|\b\d{16}\b/);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
}

for (const width of widths) {
  test(`refund partly spent purchase twice, replay changes nothing, no debt at ${width}px`, async ({
    page,
    request,
  }, testInfo) => {
    const { purchased, buyPack, spend } = await setup(page, request, width);
    const notify = await buyPack();
    expect(await purchased()).toBe(400);
    await spend(100);
    expect(await purchased()).toBe(300);

    // First refund: $2.50 of $5.00 = 200 credits.
    await notify("partially_refunded", "charge.refunded", {
      amount_minor: 250,
    });
    expect(await purchased()).toBe(100);

    // Second, distinct refund (no reversal id): $1.00 = 80 credits.
    await notify("partially_refunded", "charge.refunded", {
      amount_minor: 100,
    });
    expect(await purchased()).toBe(20);

    // A refund with an explicit id spends the last 20 credits; replaying it changes nothing.
    await notify("partially_refunded", "charge.refunded", {
      amount_minor: 25,
      reversal_id: `re_test_replay_${width}`,
    });
    expect(await purchased()).toBe(0);
    await notify("partially_refunded", "charge.refunded", {
      amount_minor: 25,
      reversal_id: `re_test_replay_${width}`,
    });
    expect(await purchased()).toBe(0);

    await page.reload();
    await expect(page.getByRole("alert")).toHaveCount(0);
    await expect(page.getByLabel("Daily free credits")).toBeVisible();
    await expect(
      page.getByText("Refund", { exact: true }).first(),
    ).toBeVisible();
    await expectNoPrivateDetails(page);
    await page.screenshot({
      path: testInfo.outputPath("credits-e7-10-refunds.png"),
      fullPage: true,
    });
  });
}

for (const width of widths) {
  test(`charge back depleted purchase shows debt, pauses paid AI, keeps unrelated pack at ${width}px`, async ({
    page,
    request,
  }, testInfo) => {
    const { authHeaders, purchased, buyPack, spend } = await setup(
      page,
      request,
      width,
    );
    await buyPack(); // unrelated pack A
    const notifyB = await buyPack(); // pack B, the latest purchase
    expect(await purchased()).toBe(800);
    await spend(400); // depletes pack B only
    expect(await purchased()).toBe(400);

    await notifyB("disputed", "charge.dispute.created", {
      amount_minor: 500,
    });
    // Pack A is untouched; pack B's shortfall becomes non-spendable debt.
    expect(await purchased()).toBe(400);

    await page.reload();
    const debtAlert = page.getByRole("alert").filter({ hasText: /debt/i });
    await expect(debtAlert).toBeVisible();
    await expect(debtAlert).toContainText("400 credits source debt");
    await expect(debtAlert).toContainText(/paid ai features paused/i);
    await expect(debtAlert).toContainText(/daily free credits remain/i);
    await expect(
      page.getByText(/refund policy|support@duolify.com/i),
    ).toBeVisible();
    await expect(page.getByLabel("Daily free credits")).toBeVisible();
    await expectNoPrivateDetails(page);

    // Keyboard: Tab reaches the credit-options control with a visible focus ring, Enter opens the shop.
    const options = page.getByRole("button", { name: "View credit options" });
    for (let i = 0; i < 30; i++) {
      await page.keyboard.press("Tab");
      if (await options.evaluate((el) => el === document.activeElement)) break;
    }
    await expect(options).toBeFocused();
    expect(
      await options.evaluate((el) => {
        const cs = getComputedStyle(el);
        return cs.outlineStyle !== "none" || cs.boxShadow !== "none";
      }),
    ).toBe(true);
    await page.keyboard.press("Enter");
    await expect(
      page.getByRole("button", { name: "Browse prices" }),
    ).toBeVisible();
    await page.getByRole("button", { name: "Browse prices" }).click();
    await expect(
      page.getByText(/paid ai features are paused pending resolution/i),
    ).toBeVisible();
    await expectNoPrivateDetails(page);

    // Paid AI reservation is blocked while debt is open.
    const paidQuoteRes = await request.post(apiBase + "/v1/quotes", {
      headers: authHeaders,
      data: { action: "lesson_content" },
    });
    const paidQuoteId = (await paidQuoteRes.json()).id;
    await request.post(apiBase + `/v1/quotes/${paidQuoteId}/accept`, {
      headers: authHeaders,
    });
    const paidReserveRes = await request.post(
      apiBase + `/v1/quotes/${paidQuoteId}/reserve`,
      { headers: authHeaders },
    );
    expect(paidReserveRes.status()).toBe(409);
    expect((await paidReserveRes.json()).error).toMatch(
      /source_debt|insufficient_credits/,
    );

    await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
    await expectNoPrivateDetails(page);
    await page.screenshot({
      path: testInfo.outputPath("credits-e7-10-debt.png"),
      fullPage: true,
    });
    await page.unrouteAll({ behavior: "wait" });
  });
}
