import { test, expect } from "./accounts-fixture";
import { courses } from "../src/mocks/handlers";

test("Exit returns to the course and reopening preserves the current slide", async ({
  page,
}) => {
  await page.route("http://localhost:8080/courses/astronomy", (route) =>
    route.fulfill({ json: courses[0] }),
  );
  await page.goto("/course/astronomy");
  await page.getByRole("link", { name: /Start lesson/ }).click();
  await page.getByRole("button", { name: "Next", exact: true }).click();
  await page.getByRole("link", { name: "Exit lesson" }).click();
  await page.getByRole("link", { name: /Start lesson/ }).click();
  await expect(page.getByText("Slide 2 of 3", { exact: true })).toBeVisible();
});

test("panel drafts and tab scroll survive resize, close and return navigation", async ({
  page,
}, testInfo) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto("/lesson/sky");
  await page.getByRole("button", { name: "Quick note", exact: true }).click();
  await page
    .getByRole("textbox", { name: "Your note" })
    .fill("A private draft about the sky");
  await page.screenshot({ path: testInfo.outputPath("desktop-panel.png") });
  await page.setViewportSize({ width: 320, height: 740 });
  const sheet = page.getByRole("dialog", { name: "Quick note", exact: true });
  await expect(sheet.getByRole("textbox")).toHaveValue(
    "A private draft about the sky",
  );
  await expect(
    sheet.getByRole("button", { name: "Save quick note unavailable" }),
  ).toBeDisabled();
  await page.screenshot({ path: testInfo.outputPath("mobile-sheet.png") });
  await page.keyboard.press("Escape");
  await page.getByRole("button", { name: "Quick note", exact: true }).click();
  await expect(sheet.getByRole("textbox")).toHaveValue(
    "A private draft about the sky",
  );
  await page.keyboard.press("Escape");
  await page.route("http://localhost:8080/courses", (route) =>
    route.fulfill({
      json: Array.from({ length: 30 }, (_, index) => ({
        ...courses[0],
        id: `course-${index}`,
        title: `Course ${index}`,
      })),
    }),
  );
  await page.goto("/courses");
  await expect(page.getByRole("article")).toHaveCount(30);
  await page.getByRole("main").evaluate((el) => {
    el.scrollTop = 600;
  });
  await expect
    .poll(() => page.getByRole("main").evaluate((el) => el.scrollTop))
    .toBe(600);
  await page
    .getByRole("navigation", { name: "Main" })
    .getByRole("link", { name: "More", exact: true })
    .click();
  await page
    .getByRole("navigation", { name: "Main" })
    .getByRole("link", { name: "Courses", exact: true })
    .click();
  await expect
    .poll(() => page.getByRole("main").evaluate((el) => el.scrollTop))
    .toBe(600);
});

test("sidebar becomes five tabs with More, preserving route draft and scroll", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.route("http://localhost:8080/courses", (route) =>
    route.fulfill({ json: courses }),
  );
  await page.goto("/courses");
  await expect(
    page
      .getByRole("navigation", { name: "Main" })
      .getByRole("link", { name: "Quick notes", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("complementary", { name: "Your progress" }),
  ).toBeVisible();
  await page.getByRole("searchbox").fill("private draft");
  await page.setViewportSize({ width: 320, height: 740 });
  await expect(page.getByRole("searchbox")).toHaveValue("private draft");
  const nav = page.getByRole("navigation", { name: "Main" });
  await expect(nav.getByRole("link")).toHaveText([
    "Learn",
    "Courses",
    "Friends",
    "Notes",
    "More",
  ]);
  await nav.getByRole("link", { name: "More", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "More", exact: true }),
  ).toBeFocused();
  await page
    .getByRole("navigation", { name: "More destinations" })
    .getByRole("link", { name: "Credits", exact: true })
    .click();
  await expect(page).toHaveURL(/\/credits$/);
  await expect(
    nav.getByRole("link", { name: "More", exact: true }),
  ).toHaveAttribute("aria-current", "page");
  await page.goBack();
  await expect(
    page.getByRole("heading", { name: "More", exact: true }),
  ).toBeVisible();
  await page.goto("/unknown/private-topic?query=private");
  await expect(
    page.getByRole("heading", { name: "This page isn't available" }),
  ).toBeFocused();
  await page.getByRole("link", { name: "Back to Courses" }).click();
  await expect(page).toHaveURL(/\/courses$/);
});

test("desktop navigation groups learning and account links and shows compact progress widgets", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto("/courses");

  const nav = page.getByRole("navigation", { name: "Main" });
  await expect(
    nav.getByRole("group", { name: "Learning" }).getByRole("link"),
  ).toHaveText(["Learn", "Courses", "Quick notes"]);
  await expect(
    nav.getByRole("group", { name: "Account" }).getByRole("link"),
  ).toHaveText(["Credits", "Profile", "Settings"]);
  await expect(nav.getByRole("separator")).toHaveCount(1);
  await expect(nav.locator(".nav-icon").first()).toHaveCSS(
    "color",
    "rgb(31, 122, 13)",
  );

  const rail = page.getByRole("complementary", { name: "Your progress" });
  await expect(rail.getByRole("progressbar")).toHaveCount(2);
  await expect(rail.getByText("3 of 7 days")).toBeVisible();
  await expect(rail.getByText("10 of 20 XP")).toBeVisible();
  await expect(rail.getByText(/\$|€|£/)).toHaveCount(0);
});

for (const mode of ["light", "dark"] as const) {
  test(`focused player panel becomes sheet, preserving slide and keyboard recovery in ${mode}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width: 1280, height: 900 });
    await page.emulateMedia({ colorScheme: mode, reducedMotion: "reduce" });
    await page.goto("/lesson/sky");
    await expect(page.getByRole("navigation", { name: "Main" })).toHaveCount(0);
    await page.getByRole("button", { name: "Next", exact: true }).click();
    await page.getByRole("button", { name: "Transcript", exact: true }).click();
    await expect(
      page.getByRole("region", { name: "Transcript", exact: true }),
    ).toBeVisible();
    await page.setViewportSize({ width: 320, height: 740 });
    const sheet = page.getByRole("dialog", { name: "Transcript", exact: true });
    await expect(sheet).toBeVisible();
    await expect(page.getByText("Slide 2 of 3", { exact: true })).toBeVisible();
    await sheet.getByRole("button", { name: "Close Transcript" }).focus();
    await page.keyboard.press("Tab");
    await expect(
      sheet.getByRole("button", { name: "Close Transcript" }),
    ).toBeFocused();
    await page.keyboard.press("Escape");
    await expect(sheet).not.toBeVisible();
    await expect(
      page.getByRole("button", { name: "Transcript", exact: true }),
    ).toBeFocused();
    await page.evaluate(() => {
      document.documentElement.style.fontSize = "200%";
    });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    await page.getByRole("link", { name: "Exit lesson" }).click();
    await expect(page).toHaveURL(/\/course\/astronomy$/);
    await page.goto("/lesson/sky/test");
    await expect(
      page.getByRole("heading", { name: "Lesson test" }),
    ).toBeVisible();
    await expect(page.getByRole("navigation", { name: "Main" })).toHaveCount(0);
  });
}

test("breakpoints and safe area reserve the full tab height at 200% text", async ({
  page,
}) => {
  await page.goto("/more");
  for (const [width, sidebar, rail] of [
    [767, false, false],
    [768, true, false],
    [1199, true, false],
    [1200, true, true],
  ] as const) {
    await page.setViewportSize({ width, height: 900 });
    await expect(page.locator(".sidebar")).toBeVisible({ visible: sidebar });
    await expect(
      page.getByRole("complementary", { name: "Your progress" }),
    ).toBeVisible({ visible: rail });
  }
  await page.setViewportSize({ width: 320, height: 740 });
  await page.evaluate(() => {
    document.documentElement.style.fontSize = "200%";
  });
  const main = await page.getByRole("main").boundingBox();
  const nav = await page
    .getByRole("navigation", { name: "Main" })
    .boundingBox();
  expect(main!.y + main!.height).toBeLessThanOrEqual(nav!.y);
  for (const link of await page
    .getByRole("navigation", { name: "Main" })
    .getByRole("link")
    .all()) {
    const box = await link.boundingBox();
    expect(box!.height).toBeGreaterThanOrEqual(44);
    expect(box!.width).toBeGreaterThanOrEqual(44);
  }
});
