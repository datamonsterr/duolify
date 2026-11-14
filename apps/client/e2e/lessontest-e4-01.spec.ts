import { test, expect } from "./accounts-fixture";

for (const mobile of [false, true]) {
  test(`generate lesson one, play, answer and pass with no completion bypass ${mobile ? "mobile" : "desktop"}`, async ({
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

    let lesson1State: "ready_to_generate" | "generating" | "current" | "done" =
      "ready_to_generate";
    let lesson2State: "locked" | "ready_to_generate" = "locked";

    const getCourse = () => ({
      id: courseId,
      title: "Astronomy for curious beginners",
      description: "Explore the night sky and constellations.",
      units: [
        {
          id: "u1",
          title: "Finding your bearings",
          lessons: [
            {
              id: lesson1Id,
              title: "A map of the night",
              goal: "Recognize night sky coordinates",
              state: lesson1State,
            },
            {
              id: lesson2Id,
              title: "Why the sky changes",
              goal: "Understand Earth rotation",
              state: lesson2State,
            },
          ],
        },
      ],
    });

    const lessonData = {
      id: lesson1Id,
      course_id: courseId,
      title: "A map of the night",
      slides: [
        {
          slide_id: "s1",
          html: '<div id="root"><h1>The Night Sky</h1><p>The sky is a map waiting to be explored.</p></div>',
          duration_ms: 3000,
          narration: "The sky is a map waiting to be explored.",
        },
        {
          slide_id: "s2",
          html: '<div id="root"><h1>Stars and Suns</h1><p>Stars are distant suns.</p></div>',
          duration_ms: 3000,
          narration: "Stars are distant suns, shining across space.",
        },
      ],
    };

    const testQuestions = [
      {
        question_id: "34871c7b-d2ed-4cda-b14c-a7c69da20611",
        topic_id: "top-stars",
        prompt: "What are stars in the night sky?",
        choices: ["Distant suns", "Atmospheric reflections", "Space crystals"],
      },
    ];

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

    const generationJobId = "gen-123";

    await page.route("**/v1/quotes", (r) => {
      if (r.request().method() === "POST") {
        return r.fulfill({
          status: 201,
          json: {
            id: "quote-123",
            action: "lesson_content",
            pricing_version: "usd-quotes-2026-11-09",
            content_ceiling: 140,
            audio_ceiling: 50,
            accepted: false,
            expired: false,
            expires_at: new Date(Date.now() + 600000).toISOString(),
          },
        });
      }
      return r.continue();
    });

    await page.route("**/v1/quotes/quote-123/accept", (r) =>
      r.fulfill({ json: { accepted: true } }),
    );

    await page.route(`**/v1/lessons/${lesson1Id}/prepare`, (r) => {
      lesson1State = "generating";
      return r.fulfill({
        status: 202,
        json: {
          id: generationJobId,
          lesson_id: lesson1Id,
          course_id: courseId,
          state: "generating",
        },
      });
    });

    await page.route(
      `**/v1/lesson-generations/${generationJobId}/events`,
      (r) => {
        lesson1State = "current";
        return r.fulfill({
          status: 200,
          headers: { "Content-Type": "text/event-stream" },
          body: 'event: stage\ndata: {"state":"generating","stage":"Generating slides"}\n\nevent: stage\ndata: {"state":"completed","stage":"Ready"}\n\n',
        });
      },
    );

    await page.route(`**/v1/lessons/${lesson1Id}`, (r) => {
      return r.fulfill({ json: lessonData });
    });

    await page.route(`**/v1/lessons/${lesson1Id}/test`, (r) => {
      expect(r.request().headers().authorization).toMatch(/^Bearer .+/);
      return r.fulfill({
        json: {
          lesson_id: lesson1Id,
          course_id: courseId,
          questions: testQuestions,
          resolved_topics: 0,
          total_topics: 1,
        },
      });
    });

    let answersSubmitted = 0;
    await page.route(`**/v1/lessons/${lesson1Id}/test/answer`, async (r) => {
      expect(r.request().headers().authorization).toMatch(/^Bearer .+/);
      answersSubmitted++;
      if (answersSubmitted === 1) {
        // Wrong answer
        return r.fulfill({
          json: {
            correct: false,
            answer_index: 0,
            explanation: "Stars are distant suns, shining across space.",
            resolved_topics: 0,
            total_topics: 1,
            retry_queue_count: 1,
            passed: false,
            retry_variant: {
              question_id: "965377bf-6d5a-4668-8417-86bb374f8049",
              topic_id: "top-stars",
              variant_of: "34871c7b-d2ed-4cda-b14c-a7c69da20611",
              is_variant: true,
              prompt: "Which objects shine as points in a clear night sky?",
              choices: ["Remote suns", "Reflected clouds", "Floating gems"],
            },
          },
        });
      } else {
        // Correct answer
        lesson1State = "done";
        lesson2State = "ready_to_generate";
        return r.fulfill({
          json: {
            correct: true,
            answer_index: 0,
            explanation: "Correct! Stars are distant suns.",
            resolved_topics: 1,
            total_topics: 1,
            retry_queue_count: 0,
            passed: true,
          },
        });
      }
    });

    // 1. Visit Course Path
    await page.goto(`/course/${courseId}`);

    // Verify ready to prepare node
    const prepareButton = page.getByRole("button", {
      name: /Prepare lesson/i,
    });
    await expect(prepareButton).toBeVisible();

    // Click prepare
    await prepareButton.click();
    await page.getByRole("button", { name: "Accept quote terms" }).click();

    // Verify generating or ready node
    const startLink = page.getByRole("link", { name: /Start lesson/i });
    await expect(startLink).toBeVisible();

    // 2. Click Start lesson
    await startLink.click();

    // Verify in lesson player
    await expect(page).toHaveURL(new RegExp(`/lesson/${lesson1Id}`));
    await expect(page.getByText("Slide 1 of 2")).toBeVisible();

    // Advance to last slide
    await page.getByRole("button", { name: "Next" }).click();
    await expect(page.getByText("Slide 2 of 2")).toBeVisible();

    // Verify last slide CTA is "Take lesson test", NOT "Finish lesson"
    await expect(
      page.getByRole("button", { name: "Finish lesson" }),
    ).not.toBeVisible();
    const takeTestButton = page
      .getByRole("button", { name: /Take lesson test/i })
      .or(page.getByRole("link", { name: /Take lesson test/i }));
    await expect(takeTestButton).toBeVisible();

    // 3. Click Take lesson test
    const manifestRequest = page.waitForRequest(
      (r) => new URL(r.url()).pathname === `/v1/lessons/${lesson1Id}/manifest`,
    );
    await takeTestButton.click();
    expect((await (await manifestRequest).allHeaders()).authorization).toMatch(
      /^Bearer .+/,
    );
    await expect(page).toHaveURL(new RegExp(`/lesson/${lesson1Id}/test`));

    // Verify Question and choices
    await expect(
      page.getByText("What are stars in the night sky?"),
    ).toBeVisible();
    const wrongChoice = page.getByLabel("Atmospheric reflections");
    const correctChoice = page.getByLabel("Remote suns");

    // Select wrong choice first
    await wrongChoice.check();
    await page.getByRole("button", { name: /Check/i }).click();

    // Verify correction sheet shown
    await expect(page.getByText(/Not quite — here's why/i)).toBeVisible();
    await expect(
      page.getByText("Stars are distant suns, shining across space."),
    ).toBeVisible();

    // Click continue
    await page.getByRole("button", { name: /Continue/i }).click();

    // Select correct choice
    await correctChoice.check();
    await page.getByRole("button", { name: /Check/i }).click();

    // Verify Correct result
    await expect(page.getByText(/Correct/i)).toBeVisible();
    await page.getByRole("button", { name: /Continue/i }).click();

    // 4. Verify Finish screen
    await expect(page.getByText(/Lesson complete!/i)).toBeVisible();

    // Return to path
    await page.getByRole("link", { name: /Back to your path/i }).click();
    await expect(page).toHaveURL(new RegExp(`/course/${courseId}`));

    // Verify Lesson 1 is now marked done and Lesson 2 is now ready_to_generate
    await expect(
      page.getByLabel(/Lesson 1, A map of the night, completed/i),
    ).toBeVisible();
    await expect(page.getByText("Completed · Review")).toBeVisible();
    expect(answersSubmitted).toBeGreaterThanOrEqual(2);
  });
}

test("hides generate/test entry points when tracer flag is disabled", async ({
  page,
}) => {
  const courseId = "c1111111-1111-4111-8111-111111111111";
  const lesson1Id = "l1111111-1111-4111-8111-111111111111";

  await page.route("**/v1/features", (r) =>
    r.fulfill({ json: { lesson_test_tracer_enabled: false } }),
  );
  await page.route("**/v1/courses", (r) =>
    r.fulfill({
      json: {
        courses: [
          {
            id: courseId,
            title: "Astronomy for curious beginners",
            description: "Explore the night sky.",
            units: [
              {
                id: "u1",
                title: "Finding your bearings",
                lessons: [
                  {
                    id: lesson1Id,
                    title: "A map of the night",
                    goal: "Recognize night sky coordinates",
                    state: "ready_to_generate",
                  },
                ],
              },
            ],
          },
        ],
        active_course_id: courseId,
      },
    }),
  );
  await page.route(`**/v1/courses/${courseId}`, (r) =>
    r.fulfill({
      json: {
        id: courseId,
        title: "Astronomy for curious beginners",
        description: "Explore the night sky.",
        units: [
          {
            id: "u1",
            title: "Finding your bearings",
            lessons: [
              {
                id: lesson1Id,
                title: "A map of the night",
                goal: "Recognize night sky coordinates",
                state: "ready_to_generate",
              },
            ],
          },
        ],
      },
    }),
  );
  await page.route("**/v1/lessons/**", (r) =>
    r.fulfill({ status: 404, json: { error: "not found" } }),
  );

  await page.goto(`/course/${courseId}`);
  // Prepare lesson button must NOT be visible
  await expect(
    page.getByRole("button", { name: /Prepare lesson/i }),
  ).not.toBeVisible();
  // Status is displayed as Locked
  await expect(page.getByText("Locked")).toBeVisible();

  // Directly navigating to test route shows Route unavailable
  await page.goto(`/lesson/${lesson1Id}/test`);
  await expect(
    page.getByRole("heading", { name: /This page isn't available/i }),
  ).toBeVisible();
});
