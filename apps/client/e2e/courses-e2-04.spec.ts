import { test, expect } from "./accounts-fixture";

for (const mobile of [false, true]) {
  test(`recover generation after disconnect/failure, retry with fresh consent, and delete course ${mobile ? "mobile" : "desktop"}`, async ({
    page,
  }) => {
    await page.setViewportSize(
      mobile ? { width: 390, height: 844 } : { width: 1280, height: 900 },
    );
    await page.emulateMedia({
      colorScheme: mobile ? "dark" : "light",
      reducedMotion: "reduce",
    });

    const draft = {
      id: "draft-e2-04",
      brief_revision: "brief-e2-04",
      revision: 2,
      outline: [
        {
          id: "u1",
          title: "My Preserved Unit",
          lessons: [
            {
              id: "l1",
              title: "My Edited Lesson",
              goal: "Understand constellations",
            },
          ],
        },
      ],
      history: [],
      confirmed: {
        draft_id: "draft-e2-04",
        revision: 2,
        content_hash: "hash-e2-04",
        outline: [
          {
            id: "u1",
            title: "My Preserved Unit",
            lessons: [
              {
                id: "l1",
                title: "My Edited Lesson",
                goal: "Understand constellations",
              },
            ],
          },
        ],
      },
    };

    let creationAttempts = 0;
    let quoteAttempts = 0;
    let creationState = "failed";
    let courseDeleted = false;
    const courseId = "c4444444-4444-4444-8444-444444444444";

    await page.route("**/v1/course-brief/config", (r) =>
      r.fulfill({
        json: { enabled: true, credits: 0, pricing_version: "fixture" },
      }),
    );
    await page.route("**/v1/course-brief/confirm", (r) =>
      r.fulfill({
        json: {
          id: "brief-e2-04",
          brief: { topic: "Astronomy" },
          expires_at: new Date(Date.now() + 600000).toISOString(),
        },
      }),
    );
    await page.route("**/v1/outline-drafts?*", (r) =>
      r.fulfill({ json: { draft } }),
    );
    await page.route("**/v1/outline-drafts/draft-e2-04", (r) =>
      r.fulfill({ json: draft }),
    );
    await page.route("**/v1/outline-drafts/draft-e2-04/confirm", (r) =>
      r.fulfill({
        json: {
          draft_id: draft.id,
          revision: draft.revision,
          content_hash: "hash-e2-04",
          outline: draft.outline,
        },
      }),
    );
    await page.route("**/v1/me/credits", (r) =>
      r.fulfill({ json: { purchased: 200, subscription: 0 } }),
    );

    await page.route("**/v1/course-creations/quote", (r) => {
      quoteAttempts++;
      return r.fulfill({
        json: {
          id: `quote-attempt-${quoteAttempts}`,
          content_ceiling: 55,
          audio_ceiling: 0,
          pricing_version: "fixture-quote",
          expires_at: new Date(Date.now() + 300000).toISOString(),
        },
      });
    });

    await page.route("**/v1/quotes/*/accept", (r) =>
      r.fulfill({ json: { accepted: true } }),
    );

    await page.route("**/v1/course-creations", (r) => {
      creationAttempts++;
      const data = r.request().postDataJSON();
      expect(data.draft_id).toBe("draft-e2-04");
      expect(data.expected_revision).toBe(2);

      return r.fulfill({
        status: 202,
        json: {
          id: "creation-e2-04",
          state: creationState,
          course_id: creationState === "enrolled" ? courseId : "",
        },
      });
    });

    await page.route("**/v1/course-creations/creation-e2-04", (r) =>
      r.fulfill({
        json: {
          id: "creation-e2-04",
          state: creationState,
          course_id: creationState === "enrolled" ? courseId : "",
        },
      }),
    );

    await page.route(
      "**/v1/course-creations/creation-e2-04/events",
      async (r) => {
        const body = `event: stage\ndata: ${JSON.stringify({
          id: "creation-e2-04",
          state: creationState,
          course_id: creationState === "enrolled" ? courseId : "",
        })}\n\n`;
        return r.fulfill({
          status: 200,
          contentType: "text/event-stream",
          body,
        });
      },
    );

    const initialCourses = () => [
      {
        id: courseId,
        title: "My Preserved Unit",
        description: "Recovered course description",
        level_override: "",
        units: [
          {
            id: "u1",
            title: "My Preserved Unit",
            lessons: [
              {
                id: "l1",
                title: "My Edited Lesson",
                goal: "Understand constellations",
                state: "ready_to_generate" as const,
              },
            ],
          },
        ],
      },
    ];

    await page.route("**/v1/courses", (r) => {
      if (r.request().method() === "GET") {
        return r.fulfill({
          json: {
            courses: courseDeleted ? [] : initialCourses(),
            active_course_id: courseDeleted ? "" : courseId,
          },
        });
      }
      return r.continue();
    });

    await page.route(`**/v1/courses/${courseId}`, (r) => {
      if (r.request().method() === "DELETE") {
        courseDeleted = true;
        return r.fulfill({
          status: 200,
          json: { deleted: true, id: courseId },
        });
      }
      if (r.request().method() === "GET") {
        if (courseDeleted) {
          return r.fulfill({ status: 404, json: { error: "not found" } });
        }
        return r.fulfill({ json: initialCourses()[0] });
      }
      return r.continue();
    });

    // 1. Initial navigation to Course Create
    await page.goto("/courses/create");
    await page.getByRole("link", { name: "Prefer a form?" }).click();
    await page.getByLabel("Topic", { exact: true }).fill("Astronomy");
    await page.getByRole("button", { name: "Confirm brief" }).click();

    // 2. Sample Preview -> Edit/Review Outline
    await page.getByRole("button", { name: "Resume saved outline" }).click();

    // 3. In Outline editor: confirm outline to reach Step 4
    await expect(
      page.getByRole("heading", { name: "Shape your outline", level: 2 }),
    ).toBeVisible();
    await expect(page.getByLabel("Title for My Preserved Unit")).toBeVisible();
    await page.getByRole("button", { name: "Confirm outline" }).click();

    // 4. In Step 4 (Create): review cost and submit
    await expect(
      page.getByRole("heading", { name: "Create your course outline" }),
    ).toBeVisible();
    await page.getByRole("button", { name: "Review creation cost" }).click();
    await page.getByRole("button", { name: /Create outline · up to/i }).click();

    // 5. Creation fails: Terminal failure releases reservation and shows Canvas S04 banner
    await expect(page.getByText("Couldn't create it")).toBeVisible();
    await expect(
      page.getByText(/Your 55 reserved credits were released/i),
    ).toBeVisible();
    await expect(page.getByText(/Your edits are saved/i)).toBeVisible();

    // Disconnect simulation: Reloading page restores the current step and preserves manual edits
    await page.reload();
    await expect(
      page.getByRole("heading", { name: "Create your course outline" }),
    ).toBeVisible();
    await expect(page.getByText(/Confirmed revision 2/i)).toBeVisible();

    // 6. Explicit retry with fresh consent
    creationState = "enrolled";
    const tryAgainBtn = page.getByRole("button", { name: "Try again" });
    await expect(tryAgainBtn).toBeVisible();
    await tryAgainBtn.click();

    // Learner must explicitly review fresh creation cost and consent again
    await page.getByRole("button", { name: "Review creation cost" }).click();
    await page.getByRole("button", { name: /Create outline · up to/i }).click();

    // 7. Successful creation navigates to course learn path
    await expect(page).toHaveURL(new RegExp(`/course/${courseId}`));
    expect(creationAttempts).toBeGreaterThanOrEqual(2);

    // 8. Navigate to Courses catalog to test course removal
    await page.goto("/courses");
    await expect(page.getByText("My Preserved Unit")).toBeVisible();

    // Remove course flow
    const removeBtn = page.getByRole("button", {
      name: /Remove course|Delete course/i,
    });
    await expect(removeBtn).toBeVisible();
    await removeBtn.click();

    // Accessible confirmation dialog
    const dialog = page.getByRole("dialog", {
      name: /Remove course|Delete course/i,
    });
    await expect(dialog).toBeVisible();
    await expect(
      dialog.getByText(/permanently purged after 30 days/i),
    ).toBeVisible();

    // Keyboard dismissal via Escape
    await page.keyboard.press("Escape");
    await expect(dialog).not.toBeVisible();

    // Reopen and confirm deletion
    await removeBtn.click();
    await expect(dialog).toBeVisible();
    await dialog
      .getByRole("button", { name: /Confirm|Delete|Remove/i })
      .click();

    // Course is immediately removed and hidden from active list
    await expect(
      page.getByText("What would you like to keep learning?"),
    ).toBeVisible();
    expect(courseDeleted).toBe(true);
  });
}
