import { test, expect, resetTokens } from "./accounts-fixture";
import { apiBase } from "./servers";

test.use({ hasTouch: true });
test.afterEach(() => resetTokens());

for (const width of [1280, 390, 320]) {
  test(`refund partly spent purchase twice and charge back depleted source at ${width}px`, async ({
    page,
    request,
  }, testInfo) => {
    await page.setViewportSize({ width, height: 900 });

    // Route pricing and checkout through fixture
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

    // Buy a pack: 400 credits for $5.00
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
    await expect(
      page.getByRole("status").filter({ hasText: "Payment pending" }),
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

    // Fulfill the purchase
    await notify("paid", "checkout.session.completed");
    await page.reload();
    await expect(
      page.getByRole("heading", { name: "Credits added", exact: true }),
    ).toBeVisible();
    await page.getByRole("button", { name: "Return to saved work" }).click();

    // Verify wallet has purchased credits (400)
    let wallet = await (
      await request.get(`${apiBase}/v1/me/credits`, { headers: authHeaders })
    ).json();
    expect(wallet.purchased).toBe(400);

    // Partly spend: spend 140 credits from the purchased pack
    const spendRes = await request.post(
      apiBase + "/test/credits/spend-remaining",
      {
        headers: authHeaders,
        data: { amount: 140 },
      },
    );
    expect(spendRes.status()).toBe(200);

    // Verify wallet purchased balance is now partly spent (400 - 140 = 260)
    wallet = await (
      await request.get(`${apiBase}/v1/me/credits`, { headers: authHeaders })
    ).json();
    expect(wallet.purchased).toBe(260);

    // 1. First partial refund: $2.50 (250 minor units out of 500, attributes 200 credits)
    // Unspent source has 260 credits -> reverses 200 credits. Remaining unspent: 60.
    const partReversalId = "re_test_part_1_" + orderId;
    await notify("partially_refunded", "charge.refunded", {
      amount_minor: 250,
      reversal_id: partReversalId,
    });

    await page.reload();
    wallet = await (
      await request.get(`${apiBase}/v1/me/credits`, { headers: authHeaders })
    ).json();
    expect(wallet.purchased).toBe(60);

    // Replay the exact same refund notification (duplicate reversal key)
    await notify("partially_refunded", "charge.refunded", {
      amount_minor: 250,
      reversal_id: partReversalId,
    });

    // Balance must NOT be deducted a second time
    wallet = await (
      await request.get(`${apiBase}/v1/me/credits`, { headers: authHeaders })
    ).json();
    expect(wallet.purchased).toBe(60);

    // 2. Spend remaining 60 credits so source is depleted
    const spendRemainingRes = await request.post(
      apiBase + "/test/credits/spend-remaining",
      {
        headers: authHeaders,
        data: { amount: 60 },
      },
    );
    expect(spendRemainingRes.status()).toBe(200);

    wallet = await (
      await request.get(`${apiBase}/v1/me/credits`, { headers: authHeaders })
    ).json();
    expect(wallet.purchased).toBe(0);

    // 3. Charge back the depleted source ($2.50 remaining = 200 credits)
    // Since source is depleted (unspent = 0), this forced reversal records 200 credits non-spendable source debt!
    await notify("disputed", "charge.dispute.created", {
      amount_minor: 250,
      dispute_id: "dp_test_depleted_" + orderId,
    });

    await page.reload();

    // Verify wallet shows debt banner matching S16/S17 design states
    const debtAlert = page.getByRole("alert").filter({ hasText: /debt/i });
    await expect(debtAlert).toBeVisible();
    await expect(debtAlert).toContainText("source debt");

    // Must show channel-correct refund/recovery path without private card details
    await expect(
      page.getByText(/refund policy|support@duolify.com/i),
    ).toBeVisible();
    await expect(page.getByText(/\*{4}|\b\d{16}\b/)).toHaveCount(0);

    // Attempting paid AI reservation while debt is active must be BLOCKED
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

    // Recovery check: daily free credits remain available and spendable
    await expect(page.getByLabel("Daily free credits")).toBeVisible();

    // Verify accessibility & media preferences
    await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);

    await page.screenshot({
      path: testInfo.outputPath("credits-e7-10-debt.png"),
      fullPage: true,
    });

    await page.unrouteAll({ behavior: "wait" });
  });
}
