import { test, expect } from "./accounts-fixture";
import { apiBase } from "./servers";
test.use({ hasTouch: true });
for (const width of [1280, 390, 320]) {
  test(`authoritative quote consent and recovery ${width}`, async ({
    page,
    request,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    await page.route("**/v1/pricing", async (route) =>
      route.fulfill({ response: await request.get(apiBase + "/v1/pricing") }),
    );
    await page.route("**/v1/quotes**", async (route) => {
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
    });
    await page.goto("/credits");
    await page.getByRole("button", { name: "Course creation costs" }).click();
    await page.getByRole("button", { name: "Get action quote" }).click();
    await expect(
      page.getByRole("heading", { name: "Review quote" }),
    ).toBeVisible();
    await expect(page.getByText("Content ceiling: 140 credits")).toBeVisible();
    await expect(page.getByText("Audio ceiling: 50 credits")).toBeVisible();
    await page.getByRole("button", { name: "Accept quote terms" }).focus();
    await page.keyboard.press("Enter");
    await expect(
      page.getByRole("status").filter({ hasText: "Terms accepted" }),
    ).toBeVisible();
    await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
  });
}

test("quote loading, offline retry, expired consent and fresh version", async ({
  page,
  request,
}) => {
  await page.setViewportSize({ width: 390, height: 900 });
  await page.route("**/v1/pricing", async (route) =>
    route.fulfill({ response: await request.get(apiBase + "/v1/pricing") }),
  );
  let mode = "loading";
  let release!: () => void;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  let version = "original-terms";
  await page.route("**/v1/quotes", async (route) => {
    if (mode === "loading") await gate;
    if (mode === "offline") {
      await route.abort("internetdisconnected");
      return;
    }
    await route.fulfill({
      json: {
        id: "quote",
        pricing_version: version.repeat(20),
        action: "lesson_content",
        currency: "USD",
        content_ceiling: 140,
        audio_ceiling: 50,
        daily_free_eligible: false,
        expires_at: new Date(
          Date.now() + (mode === "expired" ? -1000 : 600000),
        ).toISOString(),
        accepted: false,
      },
    });
  });
  await page.goto("/credits");
  await page.getByRole("button", { name: "Course creation costs" }).click();
  await expect(
    page.getByText("No quote yet. Request current terms to continue."),
  ).toBeVisible();
  await page.getByRole("button", { name: "Get action quote" }).click();
  await expect(page.getByText("Loading quote…")).toBeVisible();
  mode = "offline";
  release();
  await expect(page.getByRole("alert")).toContainText("Check your connection");
  mode = "expired";
  await page.getByRole("button", { name: "Renew quote" }).tap();
  await expect(page.getByRole("alert")).toContainText("Quote expired");
  await expect(
    page.getByRole("button", { name: "Accept quote terms" }),
  ).toHaveCount(0);
  version = "new-version";
  mode = "fresh";
  await page.getByRole("button", { name: "Renew quote" }).tap();
  await expect(page.getByText(/Original terms · new-version/)).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Accept quote terms" }),
  ).toBeEnabled();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
});
