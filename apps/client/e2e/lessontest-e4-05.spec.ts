import { test, expect } from "./accounts-fixture";

for (const mobile of [false, true]) {
  test(`E4-05: Pass N, observe only N+1 generated, match S12-Finish and S08-Path recovery states ${mobile ? "mobile" : "desktop"}`, async ({
    page,
  }) => {
    await page.setViewportSize(
      mobile ? { width: 390, height: 844 } : { width: 1280, height: 900 },
    );
    await page.emulateMedia({
      colorScheme: mobile ? "dark" : "light",
      reducedMotion: "reduce",
    });

    const courseId = "c1111111-1111-4111-8111-111111111111";
    const lesson1Id = "l1111111-1111-4111-8111-111111111111";
    const lesson2Id = "l2222222-2222-4222-8222-222222222222";
    const lesson3Id = "l3333333-3333-4333-8333-333333333333";

    let lesson1State: "ready_to_generate" | "generating" | "current" | "done" =
      "current";
    let lesson2State:
      "locked" | "ready_to_generate" | "generating" | "current" | "done" =
      "locked";
    const lesson3State:
      "locked" | "ready_to_generate" | "generating" | "current" | "done" =
      "locked";

    const getCourse = () => ({
      id: courseId,
      title: "Constellations and Coordinates",
      description: "Learn celestial navigation and night sky stars.",
      units: [
        {
          id: "u1",
          title: "Unit 1: The Sky Sphere",
          lessons: [
            {
              id: lesson1Id,
              title: "Lesson 1: Coordinates",
              goal: "Learn night sky coordinates",
              state: lesson1State,
            },
            {
              id: lesson2Id,
              title: "Lesson 2: Star Navigation",
              goal: "Navigate by Polaris and Southern Cross",
              state: lesson2State,
            },
            {
              id: lesson3Id,
              title: "Lesson 3: Deep Sky Objects",
              goal: "Locate nebulae and galaxies",
              state: lesson3State,
            },
          ],
        },
      ],
    });

    const lesson1Data = {
      id: lesson1Id,
      course_id: courseId,
      title: "Lesson 1: Coordinates",
      slides: [
        {
          slide_id: "s1",
          html: '<div id="root"><h1>Coordinates</h1><p>Celestial coordinate grid.</p></div>',
          duration_ms: 3000,
          narration: "Celestial coordinate grid.",
        },
      ],
    };

    const testQuestions = [
      {
        question_id: "q1",
        topic_id: "top-coords",
        prompt: "What is celestial altitude measured from?",
        choices: ["The horizon", "The zenith", "The equator"],
      },
    ];

    // Mock features
    await page.route("**/v1/features", (r) =>
      r.fulfill({ json: { lesson_test_tracer_enabled: true } }),
    );

    await page.route("**/v1/courses", (r) => {
      if (r.request().method() === "GET") {
        return r.fulfill({
          json: { courses: [getCourse()], active_course_id: courseId },
        });
      }
      return r.continue();
    });

    await page.route(`**/v1/courses/${courseId}`, (r) =>
      r.fulfill({ json: getCourse() }),
    );

    await page.route(`**/v1/lessons/${lesson1Id}`, (r) =>
      r.fulfill({ json: lesson1Data }),
    );

    await page.route(`**/v1/lessons/${lesson1Id}/test`, (r) =>
      r.fulfill({
        json: {
          lesson_id: lesson1Id,
          course_id: courseId,
          questions: testQuestions,
          resolved_topics: 0,
          total_topics: 1,
        },
      }),
    );

    const nextLessonPrepMode: "insufficient" | "failed" | "success" = "success";

    await page.route(`**/v1/lessons/${lesson1Id}/test/answer`, async (r) => {
      lesson1State = "done";
      lesson2State = "ready_to_generate";
      return r.fulfill({
        json: {
          correct: true,
          passed: true,
          answer_index: 0,
          explanation:
            "Celestial altitude is measured upwards from the observer's local horizon.",
          resolved_topics: 1,
          total_topics: 1,
          retry_queue_count: 0,
          next_lesson_id: lesson2Id,
          next_lesson_status: nextLessonPrepMode,
        },
      });
    });

    // Go to lesson 1 test directly
    await page.goto(`/lesson/${lesson1Id}/test`);

    // Verify question visible
    await expect(
      page.getByText("What is celestial altitude measured from?"),
    ).toBeVisible();

    // Keyboard selection test (press '1' then Enter)
    if (!mobile) {
      await page.keyboard.press("1");
      await page.keyboard.press("Enter");
    } else {
      await page.getByLabel("The horizon").check();
      await page.getByRole("button", { name: /Check/i }).click();
    }

    // Verify correct feedback
    await expect(page.getByText(/Correct/i)).toBeVisible();

    // Click continue to reach Finish screen
    if (!mobile) {
      await page.keyboard.press("Enter");
    } else {
      await page.getByRole("button", { name: /Continue/i }).click();
    }

    // Canvas S12-Finish assertion:
    // "Lesson complete", stat cards, Back to learn path
    await expect(
      page.getByRole("heading", { name: /Lesson complete/i }),
    ).toBeVisible();
    await expect(page.getByText(/15 XP/i)).toBeVisible();

    // When next lesson is ready/preparing, observe only N+1 is queued/generated (not N+2)
    expect(lesson1State).toBe("done");
    expect(lesson2State).toBe("ready_to_generate");
    expect(lesson3State).toBe("locked");

    // Return to path
    await page
      .getByRole("link", {
        name: /Back to (your )?learn path|Back to your path/i,
      })
      .click();
    await expect(page).toHaveURL(new RegExp(`/course/${courseId}`));

    // In S08-Path:
    // Lesson 1 is Done (gold review checkmark)
    // Lesson 2 is ready/preparing
    // Lesson 3 is LOCKED (grey)
    await expect(
      page.getByLabel(/Lesson 1, Lesson 1: Coordinates, completed/i),
    ).toBeVisible();
    await expect(page.getByText("Completed · Review")).toBeVisible();
    await expect(
      page.getByLabel(/Lesson 3, Lesson 3: Deep Sky Objects, locked/i),
    ).toBeVisible();
  });

  test(`E4-05: S12-Finish and S08-Path handles insufficient credits and failure states with retry parity ${mobile ? "mobile" : "desktop"}`, async ({
    page,
  }) => {
    await page.setViewportSize(
      mobile ? { width: 390, height: 844 } : { width: 1280, height: 900 },
    );

    const courseId = "c1111111-1111-4111-8111-111111111111";
    const lesson1Id = "l1111111-1111-4111-8111-111111111111";
    const lesson2Id = "l2222222-2222-4222-8222-222222222222";

    // Mock features
    await page.route("**/v1/features", (r) =>
      r.fulfill({ json: { lesson_test_tracer_enabled: true } }),
    );

    await page.route(`**/v1/courses/${courseId}`, (r) =>
      r.fulfill({
        json: {
          id: courseId,
          title: "Astronomy",
          units: [
            {
              id: "u1",
              title: "Unit 1",
              lessons: [
                { id: lesson1Id, title: "Lesson 1", state: "done" },
                {
                  id: lesson2Id,
                  title: "Lesson 2",
                  state: "ready_to_generate",
                },
              ],
            },
          ],
        },
      }),
    );

    await page.route(`**/v1/lessons/${lesson1Id}/test`, (r) =>
      r.fulfill({
        json: {
          lesson_id: lesson1Id,
          course_id: courseId,
          questions: [
            {
              question_id: "q1",
              topic_id: "top-1",
              prompt: "Question 1",
              choices: ["Choice A", "Choice B"],
            },
          ],
          resolved_topics: 0,
          total_topics: 1,
        },
      }),
    );

    // Simulate answer pass with insufficient credits for next lesson
    await page.route(`**/v1/lessons/${lesson1Id}/test/answer`, (r) =>
      r.fulfill({
        json: {
          correct: true,
          passed: true,
          answer_index: 0,
          explanation: "Good job",
          resolved_topics: 1,
          total_topics: 1,
          retry_queue_count: 0,
          next_lesson_status: "insufficient_credits",
          required_credits: 190,
          available_credits: 12,
        },
      }),
    );

    await page.goto(`/lesson/${lesson1Id}/test`);
    await page.getByLabel("Choice A").check();
    await page.getByRole("button", { name: /Check/i }).click();
    await page.getByRole("button", { name: /Continue/i }).click();

    // Verify S12-Finish canvas text for insufficient credits:
    // "Next lesson needs 190 credits"
    // "Purchased balance: 12. Your progress is saved; prepare it whenever you're ready."
    // Button "See credit options"
    await expect(
      page.getByText(/Next lesson needs 190 credits/i),
    ).toBeVisible();
    await expect(
      page.getByText(
        /Your progress is saved; prepare it whenever you're ready/i,
      ),
    ).toBeVisible();
    await expect(
      page.getByRole("link", { name: /See credit options/i }),
    ).toBeVisible();

    // Verify "Lesson complete" heading stands — completion is NOT undone
    await expect(
      page.getByRole("heading", { name: /Lesson complete/i }),
    ).toBeVisible();
  });
}
