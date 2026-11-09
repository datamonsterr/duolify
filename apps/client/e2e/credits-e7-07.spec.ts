import { test, expect, resetTokens } from "./accounts-fixture";
import { apiBase } from "./servers";
test.use({ hasTouch: true });
test.afterEach(() => resetTokens());

for (const [width, invoiceFirst] of [
  [1280, true],
  [390, false],
  [320, true],
] as const) {
  test(`monthly paid invoice at ${width}px, invoice first ${invoiceFirst}`, async ({
    page,
    request,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    await page.route(/\/v1\/(pricing$|credits\/checkouts)/, async (route) => {
      const req = route.request();
      const headers = req.headers();
      delete headers.origin;
      headers["x-test-stripe-fixture"] = "true";
      await route.fulfill({
        response: await request.fetch(apiBase + new URL(req.url()).pathname, {
          method: req.method(),
          headers,
          data: req.postData() ?? undefined,
        }),
      });
    });
    await page.goto("/credits");
    await page.getByRole("button", { name: "Browse prices" }).click();
    await page.getByRole("button", { name: "Plans", exact: true }).click();
    const catalogue = await (await request.get(`${apiBase}/v1/pricing`)).json();
    const plan = catalogue.plans[0];
    await page.getByRole("radio", { name: `${plan.name} · monthly` }).check();
    await page.getByRole("button", { name: "Preview checkout" }).click();
    await expect(
      page.getByText("Test checkout — no real payment is taken"),
    ).toBeVisible();
    const id = await page.evaluate(
      () =>
        Object.entries(sessionStorage).find(([key]) =>
          key.endsWith(":order"),
        )?.[1],
    );
    const notify = async (event: string) =>
      expect(
        (
          await request.post(`${apiBase}/test/stripe/${id}`, {
            data: { state: "paid", event },
          })
        ).ok(),
      ).toBe(true);
    const first = invoiceFirst ? "invoice.paid" : "checkout.session.completed";
    const second = invoiceFirst ? "checkout.session.completed" : "invoice.paid";
    await notify(first);
    if (!invoiceFirst) {
      await page.getByRole("button", { name: "Check payment status" }).click();
      await expect(page.getByText(/Payment pending/)).toBeVisible();
    }
    await notify(second);
    await notify("invoice.payment_succeeded");
    await page.reload();
    await expect(
      page.getByRole("heading", { name: "Plan activated", exact: true }),
    ).toBeVisible();
    await page.getByRole("button", { name: "Return to saved work" }).click();
    await expect(
      page.getByRole("heading", { name: `Your plan · ${plan.name}` }),
    ).toBeVisible();
    await expect(page.getByText(/Paid through/)).toBeVisible();
    await expect(
      page.getByRole("link", { name: "Manage billing with Stripe" }),
    ).toBeVisible();
    const tokens = await page.evaluate(() =>
      JSON.parse(sessionStorage.getItem("duolify.auth")!),
    );
    const wallet = await (
      await request.get(`${apiBase}/v1/me/credits`, {
        headers: { Authorization: `Bearer ${tokens.access_token}` },
      })
    ).json();
    expect(wallet.subscription).toBe(plan.monthly_credits);
    expect(wallet.plan.pricing_version).toBe(catalogue.version);
    const exported = await (
      await request.get(`${apiBase}/v1/me/data-export`, {
        headers: { Authorization: `Bearer ${tokens.access_token}` },
      })
    ).json();
    expect(exported.tables.credit_subscriptions).toHaveLength(1);
    expect(exported.tables.credit_subscription_terms).toHaveLength(1);
    expect(
      exported.tables.credit_payment_inbox.some(
        (event: { event_type: string }) => event.event_type === "invoice.paid",
      ),
    ).toBe(true);
    const manage = page.getByRole("link", {
      name: "Manage billing with Stripe",
    });
    if (width >= 768) {
      await manage.focus();
      await page.keyboard.press("Enter");
    } else await manage.tap();
    await expect(
      page.getByRole("heading", { name: "Manage billing with Stripe" }),
    ).toBeVisible();
    await expect(page.getByText(/Test billing only/)).toBeVisible();
    await page.getByRole("link", { name: "Return to your wallet" }).click();
    await expect(
      page.getByRole("heading", { name: `Your plan · ${plan.name}` }),
    ).toBeVisible();
    await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    await page.getByRole("button", { name: "Browse prices" }).click();
    await page.getByRole("button", { name: "Plans", exact: true }).click();
    await page.getByRole("radio", { name: `${plan.name} · monthly` }).check();
    await expect(
      page.getByRole("button", { name: "Preview checkout" }),
    ).toBeDisabled();
    await page.unrouteAll({ behavior: "wait" });
  });
}
