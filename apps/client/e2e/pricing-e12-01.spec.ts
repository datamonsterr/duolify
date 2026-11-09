import { test, expect } from "@playwright/test";
test.use({ hasTouch: true });

for (const width of [1280, 390, 320]) {
  test(`server pricing and recovery parity at ${width}px`, async ({
    page,
    request,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    await page.emulateMedia({ colorScheme: "light" });
    await page.route("**/v1/pricing", async (route) => {
      const response = await request.get("http://127.0.0.1:18081/v1/pricing");
      await route.fulfill({ response });
    });
    await page.goto("/credits");
    await expect(
      page.getByRole("heading", { name: "Credits", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByText("46 daily free credits", { exact: true }),
    ).toBeVisible();
    await expect(page.getByText("No wallet activity yet.")).toBeVisible();
    await expect(
      page.getByRole("button", { name: "See packs and plans" }),
    ).toHaveCSS("background-color", "rgb(31, 122, 13)");
    await page.getByRole("button", { name: "Browse prices" }).tap();
    await expect(page.getByText("400 credits", { exact: true })).toBeVisible();
    await page.getByRole("button", { name: "Plans", exact: true }).click();
    await expect(page.getByText("$9.99", { exact: true })).toBeVisible();
    await page.getByLabel("Yearly", { exact: true }).check();
    await expect(page.getByText("$384.00", { exact: true })).toBeVisible();
    await page.getByLabel("Plus", { exact: true }).check();
    await expect(
      page.getByRole("button", { name: "Checkout unavailable" }),
    ).toBeDisabled();
    await page.getByRole("button", { name: "Course creation costs" }).click();
    await expect(page.getByText("190 credits", { exact: true })).toBeVisible();
    await expect(page.getByText(/140 content \+ 50 narration/)).toBeVisible();
    await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
    await expect(page.getByText("190 credits", { exact: true })).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Course creation costs" }),
    ).toHaveCSS("background-color", "rgb(23, 50, 71)");
    await expect(
      page.getByRole("button", { name: "Course creation costs" }),
    ).toHaveCSS("color", "rgb(159, 211, 255)");
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    await page.getByRole("button", { name: "Wallet", exact: true }).focus();
    await page.keyboard.press("Enter");
    await expect(
      page.getByText("46 daily free credits", { exact: true }),
    ).toBeVisible();
  });
}

test("loading, unavailable and retry preserve server authority", async ({
  page,
  request,
}) => {
  let fail = true;
  await page.route("**/v1/pricing", async (route) => {
    if (fail)
      await route.fulfill({ status: 503, json: { error: "unavailable" } });
    else
      await route.fulfill({
        response: await request.get("http://127.0.0.1:18081/v1/pricing"),
      });
  });
  await page.goto("/credits");
  await expect(page.getByRole("alert")).toContainText("Prices are unavailable");
  fail = false;
  await page.getByRole("button", { name: "Try again" }).click();
  await expect(
    page.getByText("46 daily free credits", { exact: true }),
  ).toBeVisible();
});

test("loading, stale offline version and long content remain readable", async ({
  page,
  request,
}) => {
  let release!: () => void;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  await page.route("**/v1/pricing", async (route) => {
    await gate;
    const response = await request.get("http://127.0.0.1:18081/v1/pricing");
    const data = await response.json();
    data.plans[0].benefits = [
      "Offline library access and learning history export with expanded translated labels. ".repeat(
        12,
      ),
    ];
    await route.fulfill({ json: data });
  });
  await page.setViewportSize({ width: 320, height: 900 });
  await page.goto("/credits");
  await expect(page.getByRole("status")).toContainText("Loading prices");
  release();
  await expect(
    page.getByText("46 daily free credits", { exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Browse prices" }).tap();
  await page.getByRole("button", { name: "Plans", exact: true }).tap();
  await expect(
    page.getByText(/Offline library access and learning history export/),
  ).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await page.unroute("**/v1/pricing");
  await page.route("**/v1/pricing", (route) =>
    route.abort("internetdisconnected"),
  );
  await page.reload();
  await expect(
    page.getByText(/Offline · Last known prices · usd-launch-2026-11-08/),
  ).toBeVisible();
  await expect(page.getByRole("button", { name: "Try again" })).toBeEnabled();
  await page.getByRole("button", { name: "Browse prices" }).tap();
  await expect(
    page.getByRole("button", { name: "Checkout unavailable" }),
  ).toBeDisabled();
});
