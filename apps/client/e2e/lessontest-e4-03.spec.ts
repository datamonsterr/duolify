import { test, expect } from "./accounts-fixture";

for (const mobile of [false, true]) {
  test(`spaced retries, topic recap after 3 misses, and pass by resolving all topics ${mobile ? "mobile" : "desktop"}`, async ({
    page,
  }) => {
    await page.setViewportSize(
      mobile ? { width: 390, height: 844 } : { width: 1280, height: 900 },
    );
    await page.emulateMedia({
      colorScheme: mobile ? "dark" : "light",
      reducedMotion: "reduce",
    });

    const courseId = "c4444444-4444-4444-8444-444444444444";
    const lessonId = "l4444444-4444-4444-8444-444444444444";

    const getCourse = () => ({
      id: courseId,
      title: "Solar System Physics",
      description: "Master planetary motions and celestial concepts.",
      units: [
        {
          id: "u1",
          title: "Mechanics",
          lessons: [
            {
              id: lessonId,
              title: "Sun and Earth Rotation",
              goal: "Understand solar illumination and planetary spin",
              state: "current",
            },
          ],
        },
      ],
    });

    const lessonData = {
      id: lessonId,
      course_id: courseId,
      title: "Sun and Earth Rotation",
      slides: [
        {
          slide_id: "s1",
          html: '<div id="root"><h1>The Rising Sun</h1><p>Earth rotates toward the east, so the Sun first appears in the east.</p></div>',
          duration_ms: 3000,
          narration:
            "Earth rotates toward the east, so the Sun first appears in the east.",
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

    const q1Original = {
      question_id: "q1",
      topic_id: "top-sunrise",
      prompt: "What makes the Sun rise in the east?",
      choices: [
        "The Sun orbits Earth daily.",
        "Earth spins west to east.",
        "The Moon pushes it up.",
      ],
    };

    const q2Original = {
      question_id: "q2",
      topic_id: "top-orbits",
      prompt: "What shape are planetary orbits?",
      choices: ["Perfect circles", "Ellipses", "Straight lines"],
    };

    const v1Sunrise = {
      question_id: "v1-sunrise",
      topic_id: "top-sunrise",
      variant_of: "q1",
      is_variant: true,
      prompt:
        "You watch a sunrise from a beach facing east. What is really happening?",
      choices: [
        "The Sun is climbing out of the sea.",
        "Your side of Earth is turning toward the Sun.",
        "The Sun speeds up each morning.",
      ],
    };

    const v2Sunrise = {
      question_id: "v2-sunrise",
      topic_id: "top-sunrise",
      variant_of: "q1",
      is_variant: true,
      prompt:
        "A morning observer notes sunrise at 6 AM. What dynamic causes this?",
      choices: [
        "Earth rotation brings the observer into daylight.",
        "The Sun moves quickly across the sky.",
        "Atmospheric reflection shifts light.",
      ],
    };

    const v3Sunrise = {
      question_id: "v3-sunrise",
      topic_id: "top-sunrise",
      variant_of: "q1",
      is_variant: true,
      prompt: "Daybreak light first strikes the eastern slopes because:",
      choices: [
        "Earth rotates eastward into sunlight.",
        "The Sun travels from east to west.",
        "The horizon bends down.",
      ],
    };

    const v4Sunrise = {
      question_id: "v4-sunrise",
      topic_id: "top-sunrise",
      variant_of: "q1",
      is_variant: true,
      prompt: "Eastward planetary spin creates what observational phenomenon?",
      choices: [
        "Sun and stars appear to rise in the east.",
        "Ocean currents halt at dawn.",
        "Gravitational pull shifts eastward.",
      ],
    };

    const posthogEvents: {
      event: string;
      properties: Record<string, unknown>;
    }[] = [];

    // Intercept PostHog capture if network requests occur
    await page.route("**/e/**", (r) => {
      try {
        const data = JSON.parse(r.request().postData() || "{}");
        posthogEvents.push(data);
      } catch {
        // ignore parse error
      }
      return r.fulfill({ json: { status: 1 } });
    });

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
          session_id: "sess-spaced-e403",
          lesson_id: lessonId,
          course_id: courseId,
          questions: [q1Original, q2Original],
          resolved_topics: 0,
          total_topics: 2,
          min_gap: 2,
          max_variants_per_topic: 3,
          max_variants_per_test: 12,
        },
      });
    });

    let q1Attempts = 0;
    await page.route(`**/v1/lessons/${lessonId}/test/answer`, async (r) => {
      const body = r.request().postDataJSON();

      if (body.question_id === "q1") {
        q1Attempts++;
        return r.fulfill({
          json: {
            correct: false,
            answer_index: 1,
            explanation:
              "Earth turns once each day, west to east, so the Sun first appears in the east.",
            resolved_topics: 0,
            total_topics: 2,
            retry_queue_count: 1,
            passed: false,
            retry_variant: v1Sunrise,
          },
        });
      }

      if (body.question_id === "q2") {
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

      if (body.question_id === "v1-sunrise") {
        q1Attempts++;
        return r.fulfill({
          json: {
            correct: false,
            answer_index: 1,
            explanation:
              "Your side of Earth is turning toward the Sun as the planet rotates.",
            resolved_topics: 1,
            total_topics: 2,
            retry_queue_count: 1,
            passed: false,
            retry_variant: v2Sunrise,
          },
        });
      }

      if (body.question_id === "v2-sunrise") {
        q1Attempts++;
        return r.fulfill({
          json: {
            correct: false,
            answer_index: 0,
            explanation:
              "Earth rotation brings the observer into daylight each morning.",
            resolved_topics: 1,
            total_topics: 2,
            retry_queue_count: 1,
            passed: false,
            retry_variant: v3Sunrise,
          },
        });
      }

      if (body.question_id === "v3-sunrise") {
        q1Attempts++;
        // 3 consecutive wrong variants on this topic -> offer recap!
        return r.fulfill({
          json: {
            correct: false,
            answer_index: 0,
            explanation: "Earth rotates eastward into sunlight.",
            resolved_topics: 1,
            total_topics: 2,
            retry_queue_count: 1,
            passed: false,
            offer_recap: true,
            recap_slides: [lessonData.slides[0]],
            retry_variant: v4Sunrise,
          },
        });
      }

      if (body.question_id === "v4-sunrise") {
        return r.fulfill({
          json: {
            correct: true,
            answer_index: 0,
            explanation:
              "Sun and stars appear to rise in the east due to Earth's eastward spin.",
            resolved_topics: 2,
            total_topics: 2,
            retry_queue_count: 0,
            passed: true,
          },
        });
      }

      return r.fulfill({ status: 400, json: { error: "unexpected question" } });
    });

    await page.route(`**/v1/lessons/${lessonId}/test/recap*`, (r) => {
      return r.fulfill({
        json: {
          topic_id: "top-sunrise",
          slides: [lessonData.slides[0]],
        },
      });
    });

    // 1. Navigate to test
    await page.goto(`/lesson/${lessonId}/test`);

    // Verify initial Question 1 prompt and chips
    await expect(
      page.getByText("What makes the Sun rise in the east?"),
    ).toBeVisible();
    await expect(page.getByText("Resolved 0 of 2")).toBeVisible();
    await expect(page.getByText("Retry queue 0")).toBeVisible();

    // Select wrong choice for Q1
    const wrongChoiceQ1 = page.getByLabel("The Sun orbits Earth daily.");
    await wrongChoiceQ1.check();

    // Submit answer
    const checkBtn = page.getByRole("button", { name: /Check/i });
    await expect(checkBtn).toBeEnabled();
    await checkBtn.click();

    // Verify correction sheet
    await expect(page.getByText(/Not quite — here's why/i)).toBeVisible();
    await expect(
      page.getByText(
        "Earth turns once each day, west to east, so the Sun first appears in the east.",
      ),
    ).toBeVisible();
    await expect(page.getByText("Retry queue 1")).toBeVisible();

    // Click Continue
    await page.getByRole("button", { name: /Continue/i }).click();

    // 2. min_gap=2 spacing check: Q2 must appear next because enough questions remain
    await expect(
      page.getByText("What shape are planetary orbits?"),
    ).toBeVisible();

    // Answer Q2 correctly
    if (!mobile) {
      await page.keyboard.press("2");
      await page.keyboard.press("Enter");
    } else {
      await page.getByLabel("Ellipses").check();
      await page.getByRole("button", { name: /Check/i }).click();
    }

    await expect(page.getByText(/Correct/i)).toBeVisible();
    await page.getByRole("button", { name: /Continue/i }).click();

    // 3. Variant 1 for sunrise is served
    await expect(
      page.getByText(
        "You watch a sunrise from a beach facing east. What is really happening?",
      ),
    ).toBeVisible();
    await expect(
      page.getByText(/Retry variant · Same idea, fresh wording/i),
    ).toBeVisible();
    await expect(page.getByText("Resolved 1 of 2")).toBeVisible();

    // Answer Variant 1 wrongly
    await page.getByLabel("The Sun speeds up each morning.").check();
    await page.getByRole("button", { name: /Check/i }).click();
    await expect(page.getByText(/Not quite — here's why/i)).toBeVisible();
    await page.getByRole("button", { name: /Continue/i }).click();

    // 4. Variant 2 for sunrise is served
    await expect(
      page.getByText(
        "A morning observer notes sunrise at 6 AM. What dynamic causes this?",
      ),
    ).toBeVisible();

    // Answer Variant 2 wrongly
    await page.getByLabel("The Sun moves quickly across the sky.").check();
    await page.getByRole("button", { name: /Check/i }).click();
    await expect(page.getByText(/Not quite — here's why/i)).toBeVisible();
    await page.getByRole("button", { name: /Continue/i }).click();

    // 5. Variant 3 for sunrise is served (3rd consecutive variant attempt)
    await expect(
      page.getByText(
        "Daybreak light first strikes the eastern slopes because:",
      ),
    ).toBeVisible();

    // Answer Variant 3 wrongly
    await page.getByLabel("The horizon bends down.").check();
    await page.getByRole("button", { name: /Check/i }).click();
    await expect(page.getByText(/Not quite — here's why/i)).toBeVisible();

    // After max_variants_per_topic=3 consecutive wrong variants, offer recap!
    const recapBtn = page.getByRole("button", {
      name: /Review topic recap|Review taught concept|Recap slide/i,
    });
    await expect(recapBtn).toBeVisible();
    await recapBtn.click();

    // Verify recap slide content is shown
    await expect(page.getByText("The Rising Sun")).toBeVisible();
    await expect(
      page.getByText(
        "Earth rotates toward the east, so the Sun first appears in the east.",
      ),
    ).toBeVisible();

    // Verify recap does NOT resolve the topic: resolved topics is still 1 of 2
    await expect(page.getByText("Resolved 1 of 2")).toBeVisible();

    // Finish / dismiss recap to continue the test
    const returnToTestBtn = page.getByRole("button", {
      name: /Return to test|Continue test|Done reviewing/i,
    });
    await returnToTestBtn.click();

    // 6. Variant 4 for sunrise is served after recap
    await expect(
      page.getByText(
        "Eastward planetary spin creates what observational phenomenon?",
      ),
    ).toBeVisible();
    await expect(
      page.getByText(/Retry variant · Same idea, fresh wording/i),
    ).toBeVisible();

    // Answer Variant 4 correctly
    await page.getByLabel("Sun and stars appear to rise in the east.").check();
    await page.getByRole("button", { name: /Check/i }).click();

    await expect(page.getByText(/Correct/i)).toBeVisible();
    await page.getByRole("button", { name: /Continue/i }).click();

    // 7. All topics resolved and queue empty -> Finish lesson appears
    await expect(page.getByText(/Lesson complete!/i)).toBeVisible();
    expect(q1Attempts).toBe(4);
  });
}
