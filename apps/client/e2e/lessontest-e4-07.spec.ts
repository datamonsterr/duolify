import { test, expect } from "./accounts-fixture";

for (const mobile of [false, true]) {
  test(`exhaust retry pool with zero credits, recover from provider failure, and pass ${mobile ? "mobile" : "desktop"}`, async ({
    page,
  }) => {
    await page.setViewportSize(
      mobile ? { width: 390, height: 844 } : { width: 1280, height: 900 },
    );
    await page.emulateMedia({
      colorScheme: mobile ? "dark" : "light",
      reducedMotion: "reduce",
    });

    const courseId = "c4070000-0000-4000-8000-000000000001";
    const lessonId = "l4070000-0000-4000-8000-000000000001";

    const getCourse = () => ({
      id: courseId,
      title: "Atmospheric Physics",
      description: "Understand planetary rotation and optics.",
      units: [
        {
          id: "u1",
          title: "Mechanics",
          lessons: [
            {
              id: lessonId,
              title: "Planetary Spin",
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
      title: "Planetary Spin",
      slides: [
        {
          slide_id: "s1",
          html: '<div id="root"><h1>Eastward Spin</h1><p>Earth rotates eastward into sunlight.</p></div>',
          duration_ms: 3000,
          narration: "Earth rotates eastward into sunlight.",
          topic_ids: ["top-sunrise"],
        },
      ],
    };

    const q1Original = {
      question_id: "47003cdd-b2f6-4c74-a36e-6367ba8f9cf4",
      topic_id: "top-sunrise",
      prompt: "What makes the Sun rise in the east?",
      choices: [
        "The Sun orbits Earth daily.",
        "Earth spins west to east.",
        "The Moon pushes it up.",
      ],
    };

    const v1Sunrise = {
      question_id: "bc855c32-e31a-4db7-ae5c-c61dbe530bbe",
      topic_id: "top-sunrise",
      variant_of: "47003cdd-b2f6-4c74-a36e-6367ba8f9cf4",
      is_variant: true,
      prompt:
        "You watch a sunrise from a beach facing east. What is really happening?",
      choices: [
        "The Sun is climbing out of the sea.",
        "Your side of Earth is turning toward the Sun.",
        "The Sun speeds up each morning.",
      ],
    };

    // Reordered recycled variant after pool exhaustion
    const vReorderedFallback = {
      question_id: "d75d5c0a-23c8-429e-bac8-76ab4d3bdd1e",
      topic_id: "top-sunrise",
      variant_of: "47003cdd-b2f6-4c74-a36e-6367ba8f9cf4",
      is_variant: true,
      prompt:
        "You watch a sunrise from a beach facing east. What is really happening?",
      choices: [
        "Your side of Earth is turning toward the Sun.",
        "The Sun speeds up each morning.",
        "The Sun is climbing out of the sea.",
      ],
    };

    const posthogEvents: {
      event: string;
      properties: Record<string, unknown>;
    }[] = [];

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

    // Mock zero credits balance - learner has 0 credits
    await page.route("**/v1/credits/balance", (r) => {
      return r.fulfill({
        json: {
          balance: 0,
          daily_remaining: 0,
          paid_remaining: 0,
        },
      });
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
          session_id: "sess-exhaust-e407",
          lesson_id: lessonId,
          course_id: courseId,
          questions: [q1Original],
          resolved_topics: 0,
          total_topics: 1,
          min_gap: 2,
          max_variants_per_topic: 3,
          max_variants_per_test: 12,
        },
      });
    });

    let answerAttempts = 0;
    let simulateFailureOnce = true;

    await page.route(`**/v1/lessons/${lessonId}/test/answer`, async (r) => {
      const body = r.request().postDataJSON();

      if (body.question_id === "47003cdd-b2f6-4c74-a36e-6367ba8f9cf4") {
        answerAttempts++;
        return r.fulfill({
          json: {
            correct: false,
            answer_index: 1,
            explanation:
              "Earth turns once each day, west to east, so the Sun first appears in the east.",
            resolved_topics: 0,
            total_topics: 1,
            retry_queue_count: 1,
            passed: false,
            retry_variant: v1Sunrise,
          },
        });
      }

      if (body.question_id === "bc855c32-e31a-4db7-ae5c-c61dbe530bbe") {
        answerAttempts++;
        // First attempt on v1-sunrise fails with transient provider error to verify canvas recovery
        if (simulateFailureOnce) {
          simulateFailureOnce = false;
          return r.fulfill({
            status: 503,
            json: {
              error: "No approved retry pool is available",
              error_code: "retry_pool_unavailable",
            },
          });
        }

        // On retry, pool is exhausted -> recycle reordered variant for free without charging credits
        return r.fulfill({
          json: {
            correct: false,
            answer_index: 1,
            explanation:
              "Your side of Earth is turning toward the Sun as the planet rotates.",
            resolved_topics: 0,
            total_topics: 1,
            retry_queue_count: 1,
            passed: false,
            retry_variant: vReorderedFallback,
          },
        });
      }

      if (body.question_id === "d75d5c0a-23c8-429e-bac8-76ab4d3bdd1e") {
        answerAttempts++;
        return r.fulfill({
          json: {
            correct: true,
            answer_index: 0,
            explanation:
              "Your side of Earth is turning toward the Sun as Earth rotates.",
            resolved_topics: 1,
            total_topics: 1,
            retry_queue_count: 0,
            passed: true,
          },
        });
      }

      return r.fulfill({ status: 400, json: { error: "unexpected question" } });
    });

    // 1. Navigate to lesson test
    await page.goto(`/lesson/${lessonId}/test`);

    // Verify initial Question 1 prompt and state
    await expect(
      page.getByText("What makes the Sun rise in the east?"),
    ).toBeVisible();
    await expect(page.getByText("Resolved 0 of 1")).toBeVisible();
    await expect(page.getByText("Retry queue 0")).toBeVisible();

    // Check button initially disabled with helper text
    const checkBtn = page.getByRole("button", { name: /Check/i });
    await expect(page.getByText(/Select an answer to check it/i)).toBeVisible();

    // Answer Q1 incorrectly
    if (!mobile) {
      await page.keyboard.press("1");
      await page.keyboard.press("Enter");
    } else {
      await page.getByLabel("The Sun orbits Earth daily.").check();
      await checkBtn.click();
    }

    // Verify correction sheet
    await expect(page.getByText(/Not quite — here's why/i)).toBeVisible();
    await expect(page.getByText("Retry queue 1")).toBeVisible();
    await page.getByRole("button", { name: /Continue/i }).click();

    // 2. Variant 1 served from pool
    await expect(
      page.getByText(
        "You watch a sunrise from a beach facing east. What is really happening?",
      ),
    ).toBeVisible();
    await expect(
      page.getByText(/Retry variant · Same idea, fresh wording/i),
    ).toBeVisible();

    // Answer Variant 1 incorrectly with provider failure simulation
    await page.getByLabel("The Sun speeds up each morning.").check();
    await page.getByRole("button", { name: /Check/i }).click();

    // 3. Match Canvas S10-Test "Variant failure · offline waiting" state
    await expect(
      page.getByText("Couldn't prepare the retry question"),
    ).toBeVisible();
    await expect(
      page.getByText(
        "Your progress and queue are saved. No credits are charged for retries.",
      ),
    ).toBeVisible();

    // Verify recovery controls: Retry and Return to lesson
    const retryActionBtn = page.getByRole("button", { name: "Retry" });
    const returnActionBtn = page.getByRole("button", {
      name: /Return to lesson|Exit to lesson/i,
    });
    await expect(retryActionBtn).toBeVisible();
    await expect(returnActionBtn).toBeVisible();

    // Click Retry action to recover without blocking the test
    await retryActionBtn.click();

    // Now correction is shown for wrong answer on v1-sunrise
    await expect(page.getByText(/Not quite — here's why/i)).toBeVisible();
    await page.getByRole("button", { name: /Continue/i }).click();

    // 4. Exhausted pool recycled variant is served with reordered choices for free
    await expect(
      page.getByText(
        "You watch a sunrise from a beach facing east. What is really happening?",
      ),
    ).toBeVisible();
    await expect(
      page.getByText(/Retry variant · Same idea, fresh wording/i),
    ).toBeVisible();

    // Choices are reordered: choice 1 is now "Your side of Earth is turning toward the Sun."
    if (!mobile) {
      await page.keyboard.press("1");
      await page.keyboard.press("Enter");
    } else {
      await page
        .getByLabel("Your side of Earth is turning toward the Sun.")
        .check();
      await page.getByRole("button", { name: /Check/i }).click();
    }

    // 5. Correct answer resolves topic
    await expect(page.getByText(/Correct/i)).toBeVisible();
    await page.getByRole("button", { name: /Continue/i }).click();

    // 6. Test passes: all topics resolved
    await expect(page.getByText(/Lesson complete!/i)).toBeVisible();
    expect(answerAttempts).toBe(4);
  });
}
