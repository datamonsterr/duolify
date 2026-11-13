import { test, expect } from "./accounts-fixture";
import { apiBase } from "./servers";
test.use({ hasTouch: true });

for (const width of [1280, 390, 320]) {
  test(`priced exhaustion recovery and pending checkout at ${width}px`, async ({
    page,
    request,
  }, testInfo) => {
    await page.setViewportSize({ width, height: 900 });
    await page.route(
      /\/v1\/(pricing$|quotes|credits\/checkouts)/,
      async (route) => {
        const original = route.request();
        const headers = original.headers();
        delete headers.origin;
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
    await page.goto("/credits?payment=success");
    await page.getByRole("button", { name: "Course creation costs" }).click();
    await page
      .getByRole("combobox", { name: "Action", exact: true })
      .selectOption("preview");
    await page.getByRole("button", { name: "Get action quote" }).click();
    await page.getByRole("button", { name: "Accept quote terms" }).click();
    await page.getByRole("button", { name: "Reserve credits" }).click();
    await expect(page.getByRole("alert")).toContainText("Next daily grant");
    const options = page.getByRole("button", { name: "View credit options" });
    if (width >= 768) {
      await options.focus();
      await page.keyboard.press("Enter");
    } else await options.tap();
    await expect(page.getByText(/Next local reset:/)).toBeVisible();
    await expect(
      page.getByText(/Daily free credits cannot fund this action/),
    ).toBeVisible();
    await expect(page.getByRole("radio", { name: /Try/ })).not.toBeChecked();
    await page.getByRole("radio", { name: /Try/ }).check();
    await page.screenshot({
      path: testInfo.outputPath("priced-shop.png"),
      fullPage: true,
    });
    const preview = page.getByRole("button", { name: "Preview checkout" });
    if (width >= 768) {
      await preview.focus();
      await page.keyboard.press("Enter");
    } else await preview.tap();
    await expect(
      page.getByRole("status").filter({ hasText: "Payment pending" }),
    ).toBeVisible();
    const order = await page.evaluate(
      () =>
        Object.entries(sessionStorage).find(([key]) =>
          key.endsWith(":order"),
        )?.[1],
    );
    expect(order).toBeTruthy();
    // Callback success flags cannot change server truth.
    const tokens = await page.evaluate(() =>
      JSON.parse(sessionStorage.getItem("duolify.native-store-fixture")!),
    );
    const status = await request.get(
      `${apiBase}/v1/credits/checkouts/${order}?payment=success`,
      { headers: { Authorization: `Bearer ${tokens.access_token}` } },
    );
    expect((await status.json()).state).toBe("pending");
    // Status failures preserve pending state and offer a working retry.
    await page.route("**/v1/credits/checkouts/*", (route) =>
      route.fulfill({ status: 503, json: { error: "unavailable" } }),
    );
    await page.getByRole("button", { name: "Check payment status" }).click();
    await expect(page.getByRole("alert")).toContainText(
      "Payment status unavailable",
    );
    await page.unroute("**/v1/credits/checkouts/*");
    await page.getByRole("button", { name: "Check payment status" }).click();
    await expect(page.getByRole("alert")).toHaveCount(0);
    await expect(page.getByText(/Illustrative checkout/)).toBeVisible();
    await page.reload();
    await page.getByRole("button", { name: "Check payment status" }).click();
    await expect(
      page.getByRole("status").filter({ hasText: "Payment pending" }),
    ).toBeVisible();
    await expect(page.getByText("Credits added", { exact: true })).toHaveCount(
      0,
    );
    await page.getByRole("button", { name: "Cancel preview" }).click();
    await expect(
      page.getByText("No credits were added. Your draft is ready."),
    ).toBeVisible();
    await page.getByRole("button", { name: "Return to saved work" }).click();
    await expect(
      page.getByRole("button", { name: "Resume action" }),
    ).toBeVisible();
    await page.getByRole("button", { name: "Resume action" }).click();
    await expect(page.getByText(/Terms accepted/)).toBeVisible();
    await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
  });
}
