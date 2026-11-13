import { test, expect } from "./accounts-fixture";

for (const mobile of [false, true]) {
  test(`resume next day and reconnect with duplicated answers ${mobile ? "mobile" : "desktop"}`, async ({
    page,
  }) => {
    await page.setViewportSize(
      mobile ? { width: 390, height: 844 } : { width: 1280, height: 900 },
    );
    await page.emulateMedia({
      colorScheme: mobile ? "dark" : "light",
      reducedMotion: mobile ? "reduce" : "no-preference",
    });

    const courseId = "c5555555-5555-4555-8555-555555555555";
    const lessonId = "l5555555-5555-4555-8555-555555555555";

    const getCourse = () => ({
      id: courseId,
      title: "Planetary Astronomy",
      description: "Learn celestial mechanics and observational astronomy.",
      units: [
        {
          id: "u1",
          title: "The Solar System",
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
          html: '<div id="root"><h1>The Rising Sun</h1><p>Earth rotates eastward into sunlight.</p></div>',
          duration_ms: 3000,
          narration: "Earth rotates eastward into sunlight.",
          topic_ids: ["top-sunrise"],
        },
        {
          slide_id: "s2",
          html: '<div id="root"><h1>Planetary Orbits</h1><p>Orbits are ellipses.</p></div>',
          duration_ms: 3000,
          narration: "Orbits are ellipses around the sun.",
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

    let sessionState = "initial"; // initial -> mid_test_resumed -> completed
    const receivedOperations: string[] = [];
    let completionCommittedCount = 0;

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
      if (sessionState === "mid_test_resumed") {
        // Resuming next day: queue has Q2 and queued variant v1Sunrise, resolved_topics is 0
        return r.fulfill({
          json: {
            session_id: "sess-resume-e404",
            lesson_id: lessonId,
            course_id: courseId,
            questions: [q2Original, v1Sunrise],
            resolved_topics: 0,
            total_topics: 2,
            min_gap: 2,
            max_variants_per_topic: 3,
            max_variants_per_test: 12,
          },
        });
      }

      // Initial test load: Q1 and Q2
      return r.fulfill({
        json: {
          session_id: "sess-resume-e404",
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

    let offlineBlocked = false;

    await page.route(`**/v1/lessons/${lessonId}/test/answer`, async (r) => {
      if (offlineBlocked) {
        return r.abort("internetdisconnected");
      }

      const body = r.request().postDataJSON();
      const opKey = body.operation_key || body.action_key;
      if (opKey) {
        receivedOperations.push(opKey);
      }

      if (body.question_id === "q1") {
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
        completionCommittedCount++;
        return r.fulfill({
          json: {
            correct: true,
            answer_index: 1,
            explanation:
              "Sunrise occurs because Earth rotates toward the east into sunlight.",
            resolved_topics: 2,
            total_topics: 2,
            retry_queue_count: 0,
            passed: true,
            next_lesson_id: "l6666666-6666-4666-8666-666666666666",
            next_lesson_status: "ready_to_generate",
          },
        });
      }

      return r.fulfill({
        status: 400,
        json: { error: "unserved question key rejected" },
      });
    });

    // --- PHASE 1: Start test, miss Q1, then leave mid-test ---
    await page.goto(`/lesson/${lessonId}/test`);
    await expect(
      page.getByText("What makes the Sun rise in the east?"),
    ).toBeVisible();
    await expect(page.getByText("Resolved 0 of 2")).toBeVisible();

    // Select wrong choice for Q1
    if (!mobile) {
      await page.keyboard.press("1");
      await page.keyboard.press("Enter");
    } else {
      await page.getByLabel("The Sun orbits Earth daily.").check();
      await page.getByRole("button", { name: /Check/i }).click();
    }

    // Verify correction sheet
    await expect(page.getByText(/Not quite — here's why/i)).toBeVisible();
    await expect(page.getByText("Retry queue 1")).toBeVisible();

    // Learner decides to leave mid-test (e.g. exit to course)
    sessionState = "mid_test_resumed";
    await page.goto(`/course/${courseId}`);
    await expect(page.getByText("Planetary Astronomy")).toBeVisible();

    // --- PHASE 2: Return next day (resumed queue) ---
    await page.goto(`/lesson/${lessonId}/test`);

    // Verify resumed queue: Q2 appears directly with retry queue 1
    await expect(
      page.getByText("What shape are planetary orbits?"),
    ).toBeVisible();
    await expect(page.getByText("Resolved 0 of 2")).toBeVisible();
    await expect(page.getByText("Retry queue 1")).toBeVisible();

    // --- PHASE 3: Offline answer buffering ---
    offlineBlocked = true;

    // Answer Q2 while offline
    if (!mobile) {
      await page.keyboard.press("2");
      await page.keyboard.press("Enter");
    } else {
      await page.getByLabel("Ellipses").check();
      await page.getByRole("button", { name: /Check/i }).click();
    }

    // Verify S10-Test canvas "Offline variant" banner:
    // "Offline variant — X answers are waiting to be checked. You'll see results and XP when you reconnect."
    await expect(page.getByText(/Offline variant/i)).toBeVisible();
    await expect(
      page.getByText(/waiting to be checked.*reconnect/i),
    ).toBeVisible();

    // Continue to next question (retry variant v1Sunrise)
    const contBtn = page.getByRole("button", { name: /Continue/i });
    if (await contBtn.isVisible()) {
      await contBtn.click();
    }

    // Verify v1Sunrise is presented while still offline
    await expect(
      page.getByText(
        "You watch a sunrise from a beach facing east. What is really happening?",
      ),
    ).toBeVisible();

    // Answer v1Sunrise while offline
    if (!mobile) {
      await page.keyboard.press("2");
      await page.keyboard.press("Enter");
    } else {
      await page
        .getByLabel("Your side of Earth is turning toward the Sun.")
        .check();
      await page.getByRole("button", { name: /Check/i }).click();
    }

    // Since all questions are answered offline, S12-Finish canvas "Waiting to sync" is shown:
    // "Waiting to sync" — no XP or streak shown as awarded until verified.
    await expect(page.getByText("Waiting to sync")).toBeVisible();
    await expect(
      page.getByText(/We'll verify your answers when you're back online/i),
    ).toBeVisible();
    await expect(page.getByText(/Offline/i)).toBeVisible();

    // --- PHASE 4: Reconnect and idempotent replay with duplicate final answer ---
    offlineBlocked = false;

    // Trigger replay/sync upon reconnect
    const syncBtn = page.getByRole("button", {
      name: /Sync answers|Reconnect and sync|Try sync now|Sync now/i,
    });
    if (await syncBtn.isVisible()) {
      await syncBtn.click();
    } else {
      // Automatic reconnect replay via online event or window event
      await page.evaluate(() => window.dispatchEvent(new Event("online")));
    }

    // Verify verified pass screen: "Lesson complete!"
    await expect(page.getByText(/Lesson complete!/i)).toBeVisible();
    expect(receivedOperations.length).toBeGreaterThanOrEqual(1);

    // Duplicate answer check: re-sending the final answer does NOT record a second completion
    expect(completionCommittedCount).toBe(1);
  });
}
