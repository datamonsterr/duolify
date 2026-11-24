// Owner feedback 2026-11-23 (openspec simplify-ux-gestures-hotkeys): the courses toolbar is one
// row that leads to the list, and credits buttons size to content. API-free: every /v1 call is mocked.
import { readFileSync } from "node:fs";
import { expect, test, type Page } from "./native-store-fixture";

const learner = {
  id: "10000000-0000-4000-8000-000000000001",
  display_name: "Learner",
  locale: "en",
  time_zone: "UTC",
  background: "",
  analytics_opt_out: true,
  welcome_state: "completed",
  tour_state: "completed",
};
const pricing = JSON.parse(
  readFileSync(
    new URL("../src/features/pricing/fixtures/approved.json", import.meta.url),
    "utf8",
  ),
);
const course = (id: string) => ({
  id,
  title: `Course ${id}`,
  description: "Plain course",
  progress: { lessons_total: 4, lessons_done: 1, current: null },
});
const wallet = {
  available: 120,
  daily_free: {
    available: 0,
    granted: 46,
    resets_at: "2026-11-24T00:00:00Z",
    timezone: "UTC",
  },
  purchased: 120,
  subscription: 0,
  reserved: 0,
  recent_activity: [],
};

async function seed(page: Page, courses: unknown[]) {
  await page.addInitScript(() =>
    sessionStorage.setItem(
      "duolify.native-store-fixture",
      JSON.stringify({
        access_token: "layout-access",
        refresh_token: "layout-refresh",
        expires_in: 900,
      }),
    ),
  );
  await page.route("**/v1/**", (route) => route.fulfill({ json: [] }));
  await page.route("**/v1/me", (route) => route.fulfill({ json: learner }));
  await page.route("**/v1/courses", (route) =>
    route.fulfill({ json: courses }),
  );
  await page.route("**/v1/pricing", (route) =>
    route.fulfill({ json: pricing }),
  );
  await page.route("**/v1/gamification/streak", (route) =>
    route.fulfill({ json: { current_days: 3, today_done: true } }),
  );
  await page.route("**/v1/me/credits", (route) =>
    route.fulfill({ json: wallet }),
  );
}

const viewports = [
  { name: "phone", width: 390, height: 844 },
  { name: "desktop", width: 1280, height: 800 },
];
for (const { name, width, height } of viewports) {
  test.describe(`${name} ${width} by ${height}`, () => {
    test.use({ viewport: { width, height } });

    test("courses toolbar is one row and the first course is in view", async ({
      page,
    }) => {
      await seed(page, [course("a"), course("b")]);
      await page.goto("/courses");
      const toolbar = page.getByRole("toolbar", { name: "Courses" });
      await expect(toolbar).toBeVisible();
      const centers = await toolbar.locator("button, a").evaluateAll((nodes) =>
        nodes
          .filter((n) => (n as HTMLElement).offsetParent !== null)
          .map((n) => {
            const r = n.getBoundingClientRect();
            return Math.round(r.top + r.height / 2);
          }),
      );
      expect(centers.length).toBeGreaterThanOrEqual(4);
      expect(Math.max(...centers) - Math.min(...centers)).toBeLessThanOrEqual(
        2,
      );
      await expect(page.getByText("Search courses")).toHaveCount(0);
      const card = page.locator(".course-card").first();
      await expect(card).toBeVisible();
      const box = (await card.boundingBox())!;
      expect(box.y + box.height).toBeLessThanOrEqual(height);
    });

    test("empty courses show one line and the primary action", async ({
      page,
    }) => {
      await seed(page, []);
      await page.goto("/courses");
      const empty = page.locator(".course-empty");
      await expect(empty).toBeVisible();
      expect((await empty.boundingBox())!.height).toBeLessThan(200);
      await expect(
        page.getByRole("link", { name: "Create a course" }),
      ).toBeVisible();
    });

    test("credits buttons size to content and keep whole labels", async ({
      page,
    }) => {
      await seed(page, []);
      await page.goto("/credits");
      await expect(
        page.getByRole("heading", { name: "Credits", level: 1 }),
      ).toBeVisible();
      const buttons = page.locator(".pricing button:visible");
      await expect(buttons.first()).toBeVisible();
      const main = (await page.locator("main").boundingBox())!;
      const rects = await buttons.evaluateAll((nodes) =>
        nodes.map((n) => {
          const r = n.getBoundingClientRect();
          return { w: r.width, h: r.height, text: n.textContent ?? "" };
        }),
      );
      expect(rects.length).toBeGreaterThan(2);
      for (const r of rects) {
        // A label never breaks mid-word onto a second line.
        expect(r.h, r.text).toBeLessThan(64);
        if (width >= 768) expect(r.w, r.text).toBeLessThan(main.width * 0.6);
      }
      if (width >= 768) {
        const nav = page.getByRole("navigation", { name: "Credit views" });
        const tops = await nav
          .locator("button")
          .evaluateAll((n) =>
            n.map((b) => Math.round(b.getBoundingClientRect().top)),
          );
        expect(new Set(tops).size).toBe(1);
        // Refresh joins the same row instead of leading a row of its own.
        const refresh = page.getByRole("button", { name: "Refresh prices" });
        const r = (await refresh.boundingBox())!;
        const t0 = (await nav.locator("button").first().boundingBox())!;
        expect(
          Math.abs(r.y + r.height / 2 - (t0.y + t0.height / 2)),
        ).toBeLessThan(8);
      }
    });
  });
}
