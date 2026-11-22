import { test, expect } from "./accounts-fixture";
import { apiBase } from "./servers";
import { courses } from "../src/mocks/handlers";

test.use({ onboarding: true });
const dialog = (page: import("@playwright/test").Page) =>
  page.getByTestId("onboarding-tour");

test.beforeEach(async ({ page }) => {
  await page.route("**/v1/courses", (route) =>
    route.fulfill({ json: { courses, active_course_id: "astronomy" } }),
  );
  await page.route("**/courses/astronomy", (route) =>
    route.fulfill({ json: courses[0] }),
  );
  await page.route("**/v1/courses/examples", (route) =>
    route.fulfill({ json: { examples: [] } }),
  );
});

for (const width of [390, 1280]) {
  test.describe(`${width}px`, () => {
    test.use({ viewport: { width, height: 900 } });
    test("welcome once, tour forward/back, every step screenshot, second device", async ({
      page,
    }, info) => {
      await page.goto("/");
      await expect(page.getByTestId("onboarding-welcome")).toBeVisible();
      await page.screenshot({ path: info.outputPath(`${width}-welcome.png`) });
      await page
        .getByRole("button", { name: "Skip for now", exact: true })
        .click();
      await expect(dialog(page)).toBeVisible();
      const total = Number(await dialog(page).getAttribute("data-step-total"));
      expect(total).toBe(7);
      for (let i = 1; i <= total; i++) {
        await expect(dialog(page)).toHaveAttribute(
          "data-step-index",
          String(i),
        );
        await expect(
          dialog(page).getByText(`Step ${i} of ${total}`, { exact: true }),
        ).toBeVisible();
        const target = page.locator('[data-onboarding-active="true"]');
        await expect(target).toBeVisible();
        await expect(target).toHaveAttribute(
          "aria-describedby",
          /onboarding-description/,
        );
        const card = await page.getByTestId("tour-card").boundingBox();
        expect(card!.x).toBeGreaterThanOrEqual(0);
        expect(card!.x + card!.width).toBeLessThanOrEqual(width + 1);
        await page.screenshot({
          path: info.outputPath(`${width}-step-${i}.png`),
        });
        if (i === 2) {
          await dialog(page)
            .getByRole("button", { name: "Back", exact: true })
            .click();
          await expect(dialog(page)).toHaveAttribute("data-step-index", "1");
          await page.keyboard.press("ArrowRight");
          await expect(dialog(page)).toHaveAttribute("data-step-index", "2");
        }
        await dialog(page)
          .getByRole("button", {
            name: i === total ? "Done" : "Next",
            exact: true,
          })
          .click();
      }
      await expect(dialog(page)).toHaveCount(0);
      await page.reload();
      await expect(
        page.getByRole("heading", { name: courses[0].title, exact: true }),
      ).toBeVisible();
      await expect(page.getByTestId("onboarding-welcome")).toHaveCount(0);
      await expect(dialog(page)).toHaveCount(0);
      // A second device has the same server identity but no local progress
      // cache: the server record alone must keep the welcome and tour away.
      const token = JSON.parse(
        (await page.evaluate(() =>
          sessionStorage.getItem("duolify.native-store-fixture"),
        ))!,
      );
      const stored = await page.request.get(apiBase + "/v1/me/onboarding", {
        headers: { Authorization: `Bearer ${token.access_token}` },
      });
      expect(await stored.json()).toEqual({
        welcome_state: "skipped",
        tour_state: "completed",
      });
      await page.evaluate(() => localStorage.clear());
      await page.reload();
      await expect(page.getByTestId("onboarding-welcome")).toHaveCount(0);
      await expect(dialog(page)).toHaveCount(0);
    });
  });
}

test("topic enters the existing editable course brief", async ({ page }) => {
  await page.goto("/");
  await page
    .getByRole("textbox", { name: "What do you want to learn?", exact: true })
    .fill("Marine biology");
  await page
    .getByRole("button", { name: "Explore this topic", exact: true })
    .click();
  await expect(page).toHaveURL(/\/courses\/create$/);
  await expect(
    page.getByRole("textbox", { name: "Topic", exact: true }),
  ).toHaveValue("Marine biology");
});

test("Help me decide opens existing setup chat", async ({ page }) => {
  await page.goto("/");
  await page
    .getByRole("button", { name: "Help me decide", exact: true })
    .click();
  await expect(page).toHaveURL(/\/profile\/setup$/);
  await expect(page.getByTestId("onboarding-welcome")).toHaveCount(0);
});

test("examples use the agreed list/enroll contract and open the path", async ({
  page,
}) => {
  await page.route("**/v1/courses/examples", (route) =>
    route.fulfill({
      json: {
        examples: [
          {
            slug: "javascript",
            title: "JavaScript",
            description: "Start with the basics.",
            level: "beginner",
            units: 2,
            lessons: 8,
            estimated_minutes: 40,
          },
        ],
      },
    }),
  );
  await page.route("**/v1/courses/examples/javascript/enroll", (route) => {
    expect(route.request().method()).toBe("POST");
    return route.fulfill({ json: { course_id: "astronomy" } });
  });
  await page.goto("/");
  await page
    .getByRole("button", { name: "Try an example course", exact: true })
    .click();
  await page.getByRole("button", { name: /JavaScript/ }).click();
  await expect(page).toHaveURL(/\/course\/astronomy$/);
});

test("404 examples stay hidden; deep links finish before welcome", async ({
  page,
}) => {
  await page.route("**/v1/courses/examples", (route) =>
    route.fulfill({ status: 404 }),
  );
  await page.goto("/course/astronomy");
  await expect(
    page.getByRole("heading", { name: courses[0].title, exact: true }),
  ).toBeVisible();
  await expect(page.getByTestId("onboarding-welcome")).toHaveCount(0);
  await page.goto("/");
  await expect(page.getByTestId("onboarding-welcome")).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Try an example course" }),
  ).toHaveCount(0);
});

test("keyboard focus, reduced motion, skip and Settings replay", async ({
  page,
}) => {
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.goto("/");
  await page.getByRole("button", { name: "Skip for now" }).focus();
  await page.keyboard.press("Enter");
  await expect(dialog(page)).toHaveAttribute("data-step-index", "1");
  await expect(page.getByTestId("tour-card").locator("h2")).toBeFocused();
  await page.keyboard.press("Shift+Tab");
  await expect(
    dialog(page).getByRole("button", { name: "Skip tour" }),
  ).toBeFocused();
  await page.keyboard.press("Tab");
  await expect(
    dialog(page).getByRole("button", { name: "Next", exact: true }),
  ).toBeFocused();
  await page.keyboard.press("ArrowRight");
  await expect(dialog(page)).toHaveAttribute("data-step-index", "2");
  await page.keyboard.press("ArrowLeft");
  await expect(dialog(page)).toHaveAttribute("data-step-index", "1");
  await page.keyboard.press("Escape");
  await expect(dialog(page)).toHaveCount(0);
  await page.goto("/settings");
  await page
    .getByRole("button", { name: "Show me around", exact: true })
    .click();
  await expect(page).toHaveURL(/\/$/);
  await expect(dialog(page)).toBeVisible();
  await page.keyboard.press("Escape");
  await page.reload();
  await expect(dialog(page)).toHaveCount(0);
});

test("no-course learner sees five real targets, never an empty spotlight", async ({
  page,
}) => {
  await page.route("**/v1/courses", (route) =>
    route.fulfill({ json: { courses: [] } }),
  );
  await page.route("**/courses", (route) => route.fulfill({ json: [] }));
  await page.goto("/");
  await page.getByRole("button", { name: "Skip for now" }).click();
  await expect(dialog(page)).toHaveAttribute("data-step-total", "5");
  await expect(dialog(page)).toHaveAttribute("data-step-id", "credits");
});
