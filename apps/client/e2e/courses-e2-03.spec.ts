import { test, expect } from "./accounts-fixture";

for (const mobile of [false, true]) {
  test(`switch courses and override course level ${mobile ? "mobile" : "desktop"}`, async ({
    page,
  }) => {
    await page.setViewportSize(
      mobile ? { width: 390, height: 844 } : { width: 1280, height: 900 },
    );
    await page.emulateMedia({
      colorScheme: mobile ? "dark" : "light",
      reducedMotion: "reduce",
    });

    let activeCourseId = "c1111111-1111-4111-8111-111111111111";
    let course1Override = "";
    let course2Override = "intermediate";

    const course1 = () => ({
      id: "c1111111-1111-4111-8111-111111111111",
      title: "Confirmed Astronomy",
      description: "Explore the night sky and constellations.",
      level_override: course1Override,
      units: [
        {
          id: "u1",
          title: "Telescopes & Constellations",
          lessons: [
            {
              id: "l1",
              title: "Night mapping",
              goal: "Identify North Star and major constellations",
              state: "done" as const,
            },
            {
              id: "l2",
              title: "Moon phases",
              goal: "Predict moon cycle and tides",
              state: "ready_to_generate" as const,
            },
          ],
        },
      ],
    });

    const course2 = () => ({
      id: "c2222222-2222-4222-8222-222222222222",
      title: "Plant Physiology",
      description: "Living botany and plant biology.",
      level_override: course2Override,
      units: [
        {
          id: "u2",
          title: "Cellular Botany",
          lessons: [
            {
              id: "l3",
              title: "Chloroplasts",
              goal: "Understand photosynthesis mechanism",
              state: "done" as const,
            },
            {
              id: "l4",
              title: "Roots and Shoots",
              goal: "Track nutrient uptake in soil",
              state: "locked" as const,
            },
          ],
        },
      ],
    });

    await page.route("**/v1/courses", (r) => {
      if (r.request().method() === "GET") {
        return r.fulfill({
          json: {
            courses: [course1(), course2()],
            active_course_id: activeCourseId,
          },
        });
      }
      return r.continue();
    });

    await page.route(
      "**/v1/courses/c1111111-1111-4111-8111-111111111111",
      (r) => r.fulfill({ json: course1() }),
    );

    await page.route(
      "**/v1/courses/c2222222-2222-4222-8222-222222222222",
      (r) => r.fulfill({ json: course2() }),
    );

    await page.route(
      "**/v1/courses/c3333333-3333-4333-8333-333333333333",
      (r) =>
        r.fulfill({
          status: 404,
          json: { error: "not found" },
        }),
    );

    await page.route("**/v1/courses/*/select", async (r) => {
      const url = r.request().url();
      if (url.includes("c1111111-1111-4111-8111-111111111111")) {
        activeCourseId = "c1111111-1111-4111-8111-111111111111";
      } else if (url.includes("c2222222-2222-4222-8222-222222222222")) {
        activeCourseId = "c2222222-2222-4222-8222-222222222222";
      }
      return r.fulfill({ json: { active_course_id: activeCourseId } });
    });

    await page.route("**/v1/courses/*/level", async (r) => {
      const url = r.request().url();
      const body = r.request().postDataJSON() as { level?: string };
      const level = body?.level ?? "";
      if (url.includes("c1111111-1111-4111-8111-111111111111")) {
        course1Override = level;
      } else if (url.includes("c2222222-2222-4222-8222-222222222222")) {
        course2Override = level;
      }
      return r.fulfill({ json: { level_override: level } });
    });

    await page.route("**/v1/me/profile", (r) => {
      if (r.request().method() === "GET") {
        return r.fulfill({
          json: {
            learner_id: "a1111111-1111-4111-8111-111111111111",
            goal: "General science",
            topic: "Science",
            background: "Basic science",
            level: "beginner",
            locale: "en",
            time_zone: "UTC",
            daily_minutes: 15,
            daily_xp: 30,
            reminder_enabled: false,
            reminder_time: "",
            setup_completed: true,
            text_limit: 2000,
          },
        });
      }
      return r.continue();
    });

    // 1. Visit courses list
    await page.goto("/courses");
    await expect(
      page.getByRole("heading", { name: "Confirmed Astronomy" }),
    ).toBeVisible();
    await expect(
      page.getByRole("heading", { name: "Plant Physiology" }),
    ).toBeVisible();

    // Verify Course 1 is marked current
    const currentCard = page.locator("article.course-current");
    await expect(currentCard).toContainText("Confirmed Astronomy");

    // 2. Switch to Course 2 via clicking the card title link
    const course2Link = page.getByRole("link", { name: "Plant Physiology" });
    await course2Link.focus();
    await page.keyboard.press("Enter");

    await expect(page).toHaveURL(
      /course\/c2222222-2222-4222-8222-222222222222/,
    );
    await expect(
      page.getByRole("heading", { name: "Plant Physiology" }),
    ).toBeVisible();
    await expect(
      page.getByRole("heading", { name: "Cellular Botany" }),
    ).toBeVisible();

    // Verify ordered lesson titles and ordered lesson goals are visible
    await expect(page.getByText("Chloroplasts")).toBeVisible();
    await expect(
      page.getByText("Understand photosynthesis mechanism"),
    ).toBeVisible();
    await expect(page.getByText("Roots and Shoots")).toBeVisible();
    await expect(page.getByText("Track nutrient uptake in soil")).toBeVisible();

    // 3. Selection survives restart: reload and verify course 2 is still displayed on /
    await page.goto("/");
    await expect(
      page.getByRole("heading", { name: "Plant Physiology" }),
    ).toBeVisible();
    await page.reload();
    await expect(
      page.getByRole("heading", { name: "Plant Physiology" }),
    ).toBeVisible();

    // 4. Check Settings / Profile per-course level controls
    await page.goto("/profile");
    const perCourseSection = page.locator(".per-course-levels");
    await expect(perCourseSection.getByText("Per-course level")).toBeVisible();
    await expect(
      perCourseSection.getByRole("heading", { name: "Confirmed Astronomy" }),
    ).toBeVisible();
    await expect(perCourseSection.getByText(/Uses global/)).toBeVisible();

    await expect(
      perCourseSection.getByRole("heading", { name: "Plant Physiology" }),
    ).toBeVisible();
    await expect(perCourseSection.getByText(/Intermediate/i)).toBeVisible();

    // Set course 1 level override to "advanced"
    const course1OverrideBtn = page.getByRole("button", {
      name: /Override level for Confirmed Astronomy|Change level for Confirmed Astronomy/i,
    });
    await course1OverrideBtn.click();
    const selectLevel = page.getByLabel(/Level for Confirmed Astronomy/i);
    await selectLevel.selectOption("advanced");
    await page
      .getByRole("button", { name: /Save level for Confirmed Astronomy/i })
      .click();

    // Course 1 should now show Advanced
    await expect(perCourseSection.getByText(/Advanced/i)).toBeVisible();

    // Reset course 2 level back to global
    const resetCourse2Btn = page.getByRole("button", {
      name: /Reset level for Plant Physiology|Reset.*Plant Physiology/i,
    });
    await resetCourse2Btn.click();

    // Course 2 should now show Uses global
    await expect(
      perCourseSection.getByText(
        /Uses global.*Plant Physiology|Plant Physiology.*Uses global/i,
      ),
    ).toBeVisible();

    // 5. Foreign course access is neutral (404/unavailable)
    await page.goto("/course/c3333333-3333-4333-8333-333333333333");
    await expect(
      page.getByRole("heading", { name: /This page isn't available/i }),
    ).toBeVisible();

    // Responsiveness check
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
  });
}
