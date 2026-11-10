import { test, expect, resetTokens } from "./accounts-fixture";
import { apiBase } from "./servers";
test.use({ hasTouch: true });
test.afterEach(() => resetTokens());
for (const width of [1280, 390, 320]) {
  test(`subscription reconciles failed payment and stale cancellation at ${width}px`, async ({
    page,
    request,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    await page.route(
      /\/v1\/(pricing$|credits\/(checkouts|subscription))/,
      async (route) => {
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
      },
    );
    await page.goto("/credits");
    await page.getByRole("button", { name: "Browse prices" }).click();
    await page.getByRole("button", { name: "Plans", exact: true }).click();
    await page.getByRole("radio", { name: "Monthly", exact: true }).check();
    const catalogue = await (await request.get(`${apiBase}/v1/pricing`)).json();
    const plan = catalogue.plans[2];
    await page.getByRole("radio", { name: `${plan.name} · monthly` }).check();
    await expect(
      page.getByText(/credits granted each paid service month/i),
    ).toBeVisible();
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
    const notify = async (state: string, event: string) =>
      expect(
        (
          await request.post(`${apiBase}/test/stripe/${id}`, {
            data: { state, event },
          })
        ).ok(),
      ).toBe(true);
    await notify("previous_paid", "invoice.paid");
    await notify("renewed", "invoice.paid");
    await page.reload();
    await expect(
      page.getByRole("heading", { name: "Plan activated", exact: true }),
    ).toBeVisible();
    await page.getByRole("button", { name: "Return to saved work" }).click();
    await expect(page.getByText("Active · Monthly")).toBeVisible();
    await expect(page.getByText(/Next renewal/)).toBeVisible();
    const tokens = await page.evaluate(() =>
      JSON.parse(sessionStorage.getItem("duolify.native-store-fixture")!),
    );
    const wallet = async () =>
      (
        await request.get(`${apiBase}/v1/me/credits`, {
          headers: { Authorization: `Bearer ${tokens.access_token}` },
        })
      ).json();
    expect((await wallet()).subscription).toBe(2 * plan.monthly_credits);
    await notify("cancel_scheduled", "customer.subscription.updated");
    await page.reload();
    await expect(page.getByText(/Auto-renew off/)).toBeVisible();
    await notify("resumed", "customer.subscription.updated");
    await page.reload();
    await expect(page.getByText(/Auto-renew off/)).toHaveCount(0);
    await notify("failed", "invoice.payment_failed");
    await page.reload();
    await expect(page.getByText(/Payment failed ·/)).toBeVisible();
    expect((await wallet()).subscription).toBe(2 * plan.monthly_credits);
    // A stale deletion snapshot cannot erase canonical paid access.
    await notify("paid", "invoice.paid");
    await notify("stale", "customer.subscription.deleted");
    await page.reload();
    await expect(page.getByText("Active · Monthly")).toBeVisible();
    expect((await wallet()).subscription).toBe(2 * plan.monthly_credits);
    await page
      .getByRole("button", { name: "Change plan", exact: true })
      .click();
    await page
      .getByRole("combobox", { name: "New plan" })
      .selectOption(catalogue.plans[1].monthly.sku);
    await page.getByRole("button", { name: "Confirm plan change" }).click();
    await expect(page.getByText(/Switching to/)).toContainText(
      catalogue.plans[1].name,
    );
    await expect(page.getByText(/No proration. Your current/)).toBeVisible();
    expect((await wallet()).subscription).toBe(2 * plan.monthly_credits);
    await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    await page.unrouteAll({ behavior: "wait" });
  });
}
