import { readFileSync } from "node:fs";
import { test, expect, resetTokens } from "./accounts-fixture";

resetTokens();
test.afterEach(() => resetTokens());

// Sample slide of a published snapshot that carries v2 documents (generated from the pinned Cast).
const slideHtml = readFileSync(
  new URL("./fixtures/v2-slide.html", import.meta.url),
  "utf8",
);

for (const colorScheme of ["light", "dark"] as const)
  for (const mobile of [false, true])
    test(`published preview shows the v2 Character in its sample slide, ${mobile ? "390" : "1280"}px ${colorScheme}`, async ({
      page,
    }) => {
      await page.setViewportSize(
        mobile ? { width: 390, height: 844 } : { width: 1280, height: 900 },
      );
      await page.emulateMedia({ colorScheme, reducedMotion: "reduce" });
      await page.route("**/v1/public/shares/tok", (route) =>
        route.fulfill({
          json: {
            course_id: "6f1d2b9e-0c3a-4b7e-9d1a-2f4c8e5a7b01",
            title: "Astronomy",
            description: "Look up",
            version: 1,
            lesson_count: 2,
            generated_lesson_count: 1,
            creator: { username: "ada", display_name: "Ada" },
            outline: [
              { title: "Sky", lessons: [{ title: "Moon", generated: true }] },
            ],
            sample_slides: [{ id: "s1", html: slideHtml }],
            enrollment_count: 3,
          },
        }),
      );
      await page.route("**/v1/public/shares/tok/enrollment", (route) =>
        route.fulfill({ json: { enrolled: false } }),
      );
      await page.goto("/c/tok");
      await expect(
        page.getByRole("heading", { name: "Astronomy" }),
      ).toBeVisible();
      await expect(page.getByRole("alert")).toHaveCount(0);
      const frame = page.frameLocator('iframe[title="Sample slide 1"]');
      const character = frame.locator("svg.character-rig[data-pose]").first();
      await expect(character).toBeVisible();
      const box = await character.boundingBox();
      expect(box!.width).toBeGreaterThan(40);
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth,
        ),
      ).toBe(true);
    });
