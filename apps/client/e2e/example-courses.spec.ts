import { mkdirSync } from "node:fs";
import { join } from "node:path";
import { test, expect } from "./accounts-fixture";
import { apiBase } from "./servers";

// Optional: save screenshots for review (EXAMPLE_SHOTS_DIR=/path pnpm exec playwright test ...).
const shots = process.env.EXAMPLE_SHOTS_DIR;
if (shots) mkdirSync(shots, { recursive: true });

for (const mobile of [false, true]) {
  const size = mobile
    ? { width: 390, height: 844 }
    : { width: 1280, height: 900 };
  const label = mobile ? "390" : "1280";

  test(`a new learner starts a free example course ${label}px`, async ({
    page,
    request,
  }) => {
    // The web build talks to its own API origin; the browser fixture relays course calls to the
    // disposable API server, which runs the real enrollment and the real database.
    await page.route(/\/v1\/courses(\/|\?|$)/, async (route) => {
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
    });
    await page.setViewportSize(size);
    await page.emulateMedia({ reducedMotion: "reduce" });
    await page.goto("/courses");
    await expect(
      page.getByRole("heading", { name: "Start with an example" }),
    ).toBeVisible();
    const cards = page.locator(".example-courses article");
    await expect(cards).toHaveCount(2);
    await expect(cards.nth(0)).toContainText("Product Management Foundations");
    await expect(cards.nth(1)).toContainText("JavaScript Essentials");
    await expect(cards.nth(0)).toContainText(
      /6 lessons · about \d+ min · Free/,
    );
    // No horizontal scroll at the viewport width.
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);
    if (shots)
      await page
        .locator(".example-courses")
        .screenshot({ path: join(shots, `example-cards-${label}.png`) });

    await page
      .getByRole("button", { name: "Start JavaScript Essentials" })
      .click();
    await expect(page).toHaveURL(/\/course\/[0-9a-f-]{36}$/);
    const path = page.url();
    await expect(
      page.getByRole("heading", { name: "JavaScript Essentials" }),
    ).toBeVisible();
    await expect(page.getByTestId("example-label")).toHaveText(
      "Example course · Free",
    );
    const lesson = page.getByRole("link", {
      name: "Start lesson: Values and types",
    });
    await expect(lesson).toBeVisible();
    await lesson.click();
    await expect(page).toHaveURL(/\/lesson\/[0-9a-f-]{36}/);
    await expect(page.locator("iframe").first()).toBeAttached({
      timeout: 15_000,
    });
    if (shots)
      await page.screenshot({
        path: join(shots, `javascript-lesson-1-${label}.png`),
      });

    // Back on Courses the enrolled example is one ordinary course and is no longer offered;
    // the other example is still there. Enrolling again through the API returns the same course.
    await page.goto("/courses");
    await expect(
      page.getByRole("button", { name: "Start JavaScript Essentials" }),
    ).toHaveCount(0);
    await expect(
      page.getByRole("link", { name: "JavaScript Essentials" }),
    ).toHaveCount(1);
    await expect(page.locator(".example-courses article")).toHaveCount(1);
    await page
      .getByRole("button", { name: "Start Product Management Foundations" })
      .click();
    await expect(page).toHaveURL(/\/course\/[0-9a-f-]{36}$/);
    expect(page.url()).not.toBe(path);
    await expect(
      page.getByRole("link", {
        name: "Start lesson: The product manager's job",
      }),
    ).toBeVisible();
    if (shots) {
      await page
        .getByRole("link", { name: "Start lesson: The product manager's job" })
        .click();
      await expect(page.locator("iframe").first()).toBeAttached({
        timeout: 15_000,
      });
      await page.screenshot({
        path: join(shots, `product-management-lesson-1-${label}.png`),
      });
    }
  });
}
