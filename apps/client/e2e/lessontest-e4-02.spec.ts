import { test, expect } from "./accounts-fixture";

for (const mobile of [false, true]) {
  test(`miss a question and receive a pre-generated paraphrase later ${mobile ? "mobile" : "desktop"}`, async ({
    page,
  }) => {
    await page.setViewportSize(
      mobile ? { width: 390, height: 844 } : { width: 1280, height: 900 },
    );
    await page.emulateMedia({
      colorScheme: mobile ? "dark" : "light",
      reducedMotion: "reduce",
    });

    const courseId = "c2222222-2222-4222-8222-222222222222";
    const lessonId = "l2222222-2222-4222-8222-222222222222";

    const getCourse = () => ({
      id: courseId,
      title: "Solar System Explorations",
      description: "Learn about the sun, planets, and moons.",
      units: [
        {
          id: "u1",
          title: "Our Neighborhood",
          lessons: [
            {
              id: lessonId,
              title: "Sun and Earth",
              goal: "Understand solar illumination and rotation",
              state: "current",
            },
          ],
        },
      ],
    });

    const lessonData = {
      id: lessonId,
      course_id: courseId,
      title: "Sun and Earth",
      slides: [
        {
          slide_id: "s1",
          html: '<div id="root"><h1>The Rising Sun</h1><p>Earth rotates toward the east.</p></div>',
          duration_ms: 3000,
          narration: "Earth rotates toward the east.",
          topic_ids: ["top-sunrise"],
        },
        {
          slide_id: "s2",
          html: '<div id="root"><h1>Planetary Orbits</h1><p>Planets orbit in elliptical paths.</p></div>',
          duration_ms: 3000,
          narration: "Planets orbit in elliptical paths around the sun.",
          topic_ids: ["top-orbits"],
        },
      ],
    };

    // Original questions
    const q1Original = {
      question_id: "q1",
      topic_id: "top-sunrise",
      prompt: "Why does the Sun appear to rise in the east?",
      choices: [
        "The Sun moves eastward across the sky",
        "Earth rotates eastward on its axis",
        "Earth orbits backward every morning",
      ],
    };

    const q2Original = {
      question_id: "q2",
      topic_id: "top-orbits",
      prompt: "What shape are planetary orbits?",
      choices: ["Perfect circles", "Ellipses", "Straight lines"],
    };

    // Pre-generated paraphrase retry variant for q1
    const q1Variant = {
      question_id: "v1-q1",
      topic_id: "top-sunrise",
      variant_of: "q1",
      is_variant: true,
      prompt:
        "You watch a sunrise from a beach facing east. What is really happening?",
      choices: [
        "The Sun speeds up each morning.",
        "Your side of Earth is turning toward the Sun.",
        "The Sun is climbing out of the sea.",
      ],
    };

    // Mock API routes
    await page.route("**/v1/features", (r) => {
      return r.fulfill({ json: { lesson_test_tracer_enabled: true } });
    });

    await page.route("**/v1/courses", (r) => {
      if (r.request().method() === "GET") {
        return r.fulfill({
          json: { courses: [getCourse()], active_course_id: courseId },
        });
      }
      return r.continue();
    });

    await page.route(`**/v1/courses/${courseId}`, (r) => {
      return r.fulfill({ json: getCourse() });
    });

    await page.route(`**/v1/lessons/${lessonId}`, (r) => {
      return r.fulfill({ json: lessonData });
    });

    await page.route(`**/v1/lessons/${lessonId}/test`, (r) => {
      return r.fulfill({
        json: {
          session_id: "sess-123",
          lesson_id: lessonId,
          course_id: courseId,
          questions: [q1Original, q2Original],
          resolved_topics: 0,
          total_topics: 2,
        },
      });
    });

    let answersSubmitted = 0;
    await page.route(`**/v1/lessons/${lessonId}/test/answer`, async (r) => {
      answersSubmitted++;
      const body = r.request().postDataJSON();

      if (body.question_id === "q1") {
        // First submission: wrong answer
        return r.fulfill({
          json: {
            correct: false,
            answer_index: 1,
            explanation:
              "Earth rotates eastward, causing the Sun to appear to rise in the east.",
            resolved_topics: 0,
            total_topics: 2,
            retry_queue_count: 1,
            passed: false,
            retry_variant: q1Variant,
          },
        });
      }

      if (body.question_id === "q2") {
        // Second submission: question 2 answered correctly
        return r.fulfill({
          json: {
            correct: true,
            answer_index: 1,
            explanation:
              "Planetary orbits are slightly flattened circles called ellipses.",
            resolved_topics: 1,
            total_topics: 2,
            retry_queue_count: 1,
            passed: false,
          },
        });
      }

      if (body.question_id === "v1-q1") {
        // Third submission: retry variant answered correctly
        return r.fulfill({
          json: {
            correct: true,
            answer_index: 1,
            explanation:
              "Earth rotation causes sunrise as our location turns into the light.",
            resolved_topics: 2,
            total_topics: 2,
            retry_queue_count: 0,
            passed: true,
          },
        });
      }

      return r.fulfill({ status: 400, json: { error: "unexpected question" } });
    });

    // 1. Navigate directly to the lesson test
    await page.goto(`/lesson/${lessonId}/test`);

    // Verify initial Question 1 prompt and choices
    await expect(
      page.getByText("Why does the Sun appear to rise in the east?"),
    ).toBeVisible();

    // Verify chips: Resolved 0 of 2, Retry queue 0
    await expect(page.getByText("Resolved 0 of 2")).toBeVisible();
    await expect(page.getByText("Retry queue 0")).toBeVisible();

    // Select wrong choice for Question 1
    const wrongChoiceQ1 = page.getByLabel(
      "The Sun moves eastward across the sky",
    );
    await wrongChoiceQ1.check();

    // Check answer
    const checkBtn = page.getByRole("button", { name: /Check/i });
    await expect(checkBtn).toBeEnabled();
    await checkBtn.click();

    // Verify correction sheet
    await expect(page.getByText(/Not quite — here's why/i)).toBeVisible();
    await expect(
      page.getByText(
        "Earth rotates eastward, causing the Sun to appear to rise in the east.",
      ),
    ).toBeVisible();

    // Queue count increments
    await expect(page.getByText("Retry queue 1")).toBeVisible();

    // Click Continue
    const continueBtn = page.getByRole("button", { name: /Continue/i });
    await continueBtn.click();

    // 2. Question 2 must appear next (min_gap spacing: variant is not served immediately!)
    await expect(
      page.getByText("What shape are planetary orbits?"),
    ).toBeVisible();

    // Select correct choice for Question 2
    if (!mobile) {
      // Keyboard interaction test on desktop: press '2' for second choice
      await page.keyboard.press("2");
      await page.keyboard.press("Enter");
    } else {
      const correctChoiceQ2 = page.getByLabel("Ellipses");
      await correctChoiceQ2.check();
      await page.getByRole("button", { name: /Check/i }).click();
    }

    // Correct feedback for Q2
    await expect(page.getByText(/Correct/i)).toBeVisible();
    await page.getByRole("button", { name: /Continue/i }).click();

    // 3. Now the pre-generated Retry variant for Q1 is served!
    await expect(
      page.getByText(
        "You watch a sunrise from a beach facing east. What is really happening?",
      ),
    ).toBeVisible();

    // Verify Retry variant badge matching canvas S10-Test
    await expect(
      page.getByText(/Retry variant · Same idea, fresh wording/i),
    ).toBeVisible();

    // Verify chips: Resolved 1 of 2, Retry queue 1
    await expect(page.getByText("Resolved 1 of 2")).toBeVisible();
    await expect(page.getByText("Retry queue 1")).toBeVisible();

    // Select correct answer for the variant: "Your side of Earth is turning toward the Sun."
    const correctVariantChoice = page.getByLabel(
      "Your side of Earth is turning toward the Sun.",
    );
    await correctVariantChoice.check();
    await page.getByRole("button", { name: /Check/i }).click();

    // Verify Correct result sheet
    await expect(page.getByText(/Correct/i)).toBeVisible();
    await page.getByRole("button", { name: /Continue/i }).click();

    // 4. Test passes and shows completion view
    await expect(page.getByText(/Lesson complete!/i)).toBeVisible();
    expect(answersSubmitted).toBe(3);
  });
}
