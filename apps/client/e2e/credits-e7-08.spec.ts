import { test, expect, resetTokens } from "./accounts-fixture";
import { apiBase } from "./servers";
test.use({ hasTouch: true });
test.afterEach(() => resetTokens());
for (const width of [1280, 390, 320]) {
  test(`annual allowance survives cancellation and catches up at ${width}px`, async ({
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
    await page.getByRole("radio", { name: "Yearly", exact: true }).check();
    const catalogue = await (await request.get(`${apiBase}/v1/pricing`)).json();
    const plan = catalogue.plans[0];
    await page.getByRole("radio", { name: `${plan.name} · yearly` }).check();
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
    await notify("paid", "invoice.paid");
    await page.reload();
    await expect(
      page.getByRole("heading", { name: "Plan activated", exact: true }),
    ).toBeVisible();
    await page.getByRole("button", { name: "Return to saved work" }).click();
    await expect(page.getByText("Active · Annual")).toBeVisible();
    await expect(page.getByText(/Next allowance/)).toBeVisible();
    const tokens = await page.evaluate(() =>
      JSON.parse(sessionStorage.getItem("duolify.auth")!),
    );
    const wallet = async () =>
      (
        await request.get(`${apiBase}/v1/me/credits`, {
          headers: { Authorization: `Bearer ${tokens.access_token}` },
        })
      ).json();
    expect((await wallet()).subscription).toBe(plan.monthly_credits);
    const exported = await (
      await request.get(`${apiBase}/v1/me/data-export`, {
        headers: { Authorization: `Bearer ${tokens.access_token}` },
      })
    ).json();
    expect(exported.tables.credit_subscription_schedule).toHaveLength(12);
    await page
      .getByRole("link", { name: "Manage billing with Stripe" })
      .focus();
    await page.keyboard.press("Enter");
    await expect(
      page.getByRole("heading", { name: "Manage billing with Stripe" }),
    ).toBeVisible();
    await page.getByRole("link", { name: "Return to your wallet" }).click();
    await notify("cancel_scheduled", "customer.subscription.updated");
    await page.reload();
    await expect(page.getByText(/Auto-renew off/)).toBeVisible();
    for (let repeat = 0; repeat < 2; repeat++) {
      expect(
        (
          await request.post(`${apiBase}/test/annual/${id}`, {
            data: { month: 3 },
          })
        ).ok(),
      ).toBe(true);
      expect((await wallet()).subscription).toBe(4 * plan.monthly_credits);
    }
    await notify("refunded", "invoice.updated");
    expect(
      (
        await request.post(`${apiBase}/test/annual/${id}`, {
          data: { month: 11 },
        })
      ).ok(),
    ).toBe(true);
    expect((await wallet()).subscription).toBe(4 * plan.monthly_credits);
    await page.reload();
    await expect(
      page.getByText("Refunded · issued credits remain available"),
    ).toBeVisible();
    await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    await page.unrouteAll({ behavior: "wait" });
  });
}
