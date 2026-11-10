import { test, expect } from "./accounts-fixture";
import { apiBase } from "./servers";
import { test as publicTest } from "@playwright/test";

for (const mobile of [false, true]) {
  test.describe(mobile ? "touch" : "keyboard", () => {
    test.use({ hasTouch: mobile });
    test(`shared demo preserves creation ${mobile ? "mobile" : "desktop"}`, async ({
      page,
      request,
    }) => {
      await page.setViewportSize(
        mobile ? { width: 390, height: 844 } : { width: 1280, height: 900 },
      );
      await page.emulateMedia({
        colorScheme: mobile ? "dark" : "light",
        reducedMotion: "reduce",
      });
      let paid = 0;
      let quotes = 0;
      await page.route(
        /\/v1\/(course-brief\/|courses\/shared-demo)/,
        async (route) => {
          const original = route.request();
          const target = new URL(original.url());
          target.host = new URL(apiBase).host;
          const headers = original.headers();
          delete headers.origin;
          await route.fulfill({
            response: await request.fetch(target.href, {
              method: original.method(),
              headers,
              data: original.postData() ?? undefined,
            }),
          });
        },
      );
      await page.route("**/v1/me/credits", (route) =>
        route.fulfill({ json: { purchased: 0, subscription: 0 } }),
      );
      await page.route("**/v1/courses/preview", (route) => {
        paid++;
        return route.abort();
      });
      await page.route("**/v1/quotes", (route) => {
        quotes++;
        return route.fulfill({
          json: {
            id: `quote-${quotes}`,
            content_ceiling: 25,
            pricing_version: "unapproved-fixture",
            expires_at: new Date(Date.now() + 300000).toISOString(),
          },
        });
      });
      await page.goto("/courses/create");
      await page.getByRole("link", { name: "Prefer a form?" }).click();
      await page
        .getByLabel("Topic", { exact: true })
        .fill("My private astronomy topic");
      await page
        .getByLabel("Maths", { exact: true })
        .fill("No equations please");
      await page
        .getByRole("button", { name: "Confirm brief", exact: true })
        .click();
      await page
        .getByRole("button", { name: "Review preview cost", exact: true })
        .click();
      await expect(
        page.getByText("unapproved-fixture", { exact: false }),
      ).toBeVisible();
      const trigger = page.getByRole("button", {
        name: "Explore shared example",
        exact: true,
      });
      if (mobile) await trigger.tap();
      else {
        await trigger.focus();
        await trigger.press("Enter");
      }
      await expect(
        page.getByRole("heading", { name: "Shared example", exact: true }),
      ).toBeFocused();
      await expect(
        page.getByText("A fixed authored example", { exact: false }),
      ).toBeVisible();
      await expect(
        page.locator('iframe[title="Shared example slide 1"]'),
      ).toHaveAttribute("sandbox", "allow-scripts");
      await expect(
        page.getByRole("heading", { name: "Reading the sky" }),
      ).toBeVisible();
      expect(paid).toBe(0);
      expect(quotes).toBe(1);
      await page
        .getByRole("button", { name: "Next example", exact: true })
        .click();
      await expect(
        page.locator('iframe[title="Shared example slide 2"]'),
      ).toBeVisible();
      await page
        .getByRole("button", { name: "Previous example", exact: true })
        .click();
      await page
        .getByRole("button", { name: "Play example", exact: true })
        .click();
      await expect(
        page.getByRole("button", { name: "Pause example", exact: true }),
      ).toBeVisible();
      await page
        .getByRole("button", { name: "Return to personalized creation" })
        .click();
      await expect(
        page.getByRole("button", {
          name: "Explore shared example",
          exact: true,
        }),
      ).toBeFocused();
      await expect(
        page.getByText("unapproved-fixture", { exact: false }),
      ).toHaveCount(0);
      await page
        .getByRole("button", { name: "Review preview cost", exact: true })
        .click();
      await expect.poll(() => quotes).toBe(2);
      expect(paid).toBe(0);
      await page
        .getByRole("button", { name: "Back to brief", exact: true })
        .click();
      await expect(page.getByLabel("Topic", { exact: true })).toHaveValue(
        "My private astronomy topic",
      );
      await expect(page.getByLabel("Maths", { exact: true })).toHaveValue(
        "No equations please",
      );
      await expect(page.locator("body")).toHaveJSProperty(
        "scrollWidth",
        await page.locator("body").evaluate((el) => el.clientWidth),
      );
    });
  });
}

publicTest(
  "welcome demo recovers from offline without sign-in or paid requests",
  async ({ page, request }) => {
    let calls = 0;
    await page.route("**/v1/courses/shared-demo", async (route) => {
      calls++;
      expect(route.request().headers().authorization).toBeUndefined();
      expect(route.request().url()).not.toContain("?");
      if (calls === 1) return route.abort("internetdisconnected");
      await route.fulfill({
        response: await request.get(`${apiBase}/v1/courses/shared-demo`),
      });
    });
    await page.goto("/sign-in");
    await page
      .getByRole("button", { name: "Explore shared example", exact: true })
      .click();
    await expect(page.getByRole("alert")).toContainText(
      "Shared example unavailable",
    );
    await page.getByRole("button", { name: "Retry shared example" }).click();
    await expect(
      page.getByRole("heading", { name: "Reading the sky" }),
    ).toBeVisible();
    await page.getByRole("button", { name: "Return to welcome" }).click();
    await expect(
      page.getByRole("button", { name: "Explore shared example", exact: true }),
    ).toBeFocused();
    await expect(
      page.getByRole("heading", {
        name: "Learn any topic, a little at a time.",
      }),
    ).toBeVisible();
  },
);
