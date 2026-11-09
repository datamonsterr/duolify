import { test, expect } from "./accounts-fixture";
import { apiBase } from "./servers";
test.use({ hasTouch: true });

for (const width of [1280, 390, 320]) {
  test(`signed delayed pack fulfillment at ${width}px`, async ({
    page,
    request,
  }, testInfo) => {
    await page.setViewportSize({ width, height: 900 });
    await page.route(/\/v1\/(pricing$|credits\/checkouts)/, async (route) => {
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
    });
    await page.goto("/credits?payment=success");
    const tokens = await page.evaluate(() =>
      JSON.parse(sessionStorage.getItem("duolify.auth")!),
    );
    const authHeaders = { Authorization: `Bearer ${tokens.access_token}` };
    const before = await (
      await request.get(`${apiBase}/v1/me/credits`, { headers: authHeaders })
    ).json();
    const catalogue = await (await request.get(`${apiBase}/v1/pricing`)).json();
    await page.getByRole("button", { name: "Browse prices" }).click();
    await page.getByRole("radio", { name: /Try/ }).check();
    const buy = page.getByRole("button", { name: "Preview checkout" });
    await buy.focus();
    if (width >= 768) await page.keyboard.press("Enter");
    else await buy.tap();
    await expect(
      page.getByText("Test checkout — no real payment is taken"),
    ).toBeVisible();
    await expect(
      page.getByRole("status").filter({ hasText: "Payment pending" }),
    ).toBeVisible();
    await page.screenshot({
      path: testInfo.outputPath("checkout-pending.png"),
      fullPage: true,
    });
    const id = await page.evaluate(
      () =>
        Object.entries(sessionStorage).find(([key]) =>
          key.endsWith(":order"),
        )?.[1],
    );
    expect(id).toBeTruthy();
    const notify = async (state: string, event: string) => {
      const response = await request.post(`${apiBase}/test/stripe/${id}`, {
        data: { state, event },
      });
      expect(response.ok()).toBe(true);
    };
    await notify("unpaid", "checkout.session.completed");
    await page.getByRole("button", { name: "Check payment status" }).click();
    await expect(page.getByText(/Payment pending/)).toBeVisible();
    await notify("failed", "checkout.session.async_payment_failed");
    await page.getByRole("button", { name: "Check payment status" }).click();
    await expect(page.getByRole("alert")).toContainText(
      "Payment didn't complete",
    );
    await expect(
      page.getByRole("button", { name: "Choose another option" }),
    ).toBeVisible();
    await notify("expired", "checkout.session.expired");
    await page.getByRole("button", { name: "Check payment status" }).click();
    await expect(page.getByRole("alert")).toContainText("Checkout expired");
    await expect(
      page.getByRole("heading", { name: "Credits added", exact: true }),
    ).toHaveCount(0);
    await notify("paid", "checkout.session.async_payment_succeeded");
    await notify("paid", "checkout.session.completed");
    await page.reload();
    await expect(
      page.getByRole("heading", { name: "Credits added", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("heading", { name: "Credits added", exact: true }),
    ).toBeFocused();
    await page.screenshot({
      path: testInfo.outputPath("checkout-paid.png"),
      fullPage: true,
    });
    await notify("paid", "checkout.session.expired");
    await page.getByRole("button", { name: "Check payment status" }).click();
    await expect(
      page.getByRole("heading", { name: "Credits added", exact: true }),
    ).toBeVisible();
    await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    await page.getByRole("button", { name: "Return to saved work" }).click();
    await expect(
      page.getByRole("button", { name: "Browse prices" }),
    ).toBeVisible();
    const after = await (
      await request.get(`${apiBase}/v1/me/credits`, { headers: authHeaders })
    ).json();
    expect(after.purchased).toBe(before.purchased + catalogue.packs[0].credits);
    await page.unrouteAll({ behavior: "wait" });
  });
}
