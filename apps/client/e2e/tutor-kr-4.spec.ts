import { test, expect } from "./accounts-fixture";

const course = "c4470000-0000-4000-8000-000000000001";
const lesson = "a4470000-0000-4000-8000-000000000001";
const testUrl = `/lesson/${lesson}/test`;
const tutor = `**/v1/courses/${course}/tutor`;
const teachBack = `**/v1/courses/${course}/lessons/${lesson}/teach-back`;
const correctChoice = "Earth spins on its axis";

test.beforeEach(async ({ page }) => {
  await page.route("**/v1/features", (route) =>
    route.fulfill({ json: { lesson_test_tracer_enabled: true } }),
  );
  await page.route(`**/v1/lessons/${lesson}/manifest`, (route) =>
    route.fulfill({ json: {} }),
  );
});

for (const assessment of ["test", "retry"] as const) {
  test(`an unanswered ${assessment} question can receive a hint`, async ({
    page,
  }) => {
    const questionId = assessment === "test" ? "sunrise" : "sunrise-retry";
    await page.route(`**/v1/lessons/${lesson}/test`, (route) =>
      route.fulfill({
        json: {
          course_id: course,
          questions: [
            {
              question_id: questionId,
              topic_id: "rotation",
              prompt: "Why does the Sun appear to rise?",
              choices: [correctChoice, "The Sun orbits Earth every day"],
              ...(assessment === "retry"
                ? { is_variant: true, variant_of: "sunrise" }
                : {}),
            },
          ],
        },
      }),
    );
    await page.route(`${tutor}/thread`, (route) =>
      route.fulfill({ json: { thread: null, messages: [] } }),
    );
    await page.route(`${tutor}/personas*`, (route) =>
      route.fulfill({ json: { cast_source: "default", tutors: [] } }),
    );
    const questionRef = {
      lesson_id: lesson,
      slide_index: 0,
      question_id: questionId,
    };
    await page.route(`${tutor}/quote`, (route) => {
      expect(route.request().postDataJSON().lesson).toEqual(questionRef);
      return route.fulfill({
        json: {
          id: "hint-quote",
          content_ceiling: 5,
          pricing_version: "test",
          daily_free_eligible: true,
          expires_at: new Date(Date.now() + 600000).toISOString(),
        },
      });
    });
    await page.route("**/v1/quotes/hint-quote/accept", (route) =>
      route.fulfill({ json: { accepted: true } }),
    );
    await page.route(`${tutor}/messages`, (route) => {
      expect(route.request().postDataJSON()).toMatchObject({
        lesson: questionRef,
        message: "What is the answer?",
      });
      return route.fulfill({
        contentType: "text/event-stream",
        body:
          [
            'event: thread\ndata: {"thread_id":"t1"}',
            'event: delta\ndata: {"text":"Picture where you are standing during sunrise."}',
            'event: done\ndata: {"message_id":"m1","thread_id":"t1","replayed":false}',
          ].join("\n\n") + "\n\n",
      });
    });

    await page.goto(testUrl);
    await page.getByRole("radio", { name: correctChoice }).check();
    await page.getByRole("button", { name: "Ask the tutor" }).press("Enter");
    const message = page.getByRole("textbox", { name: "Message" });
    await message.fill("What is the answer?");
    await expect(page.getByText(/up to 5 credits/i)).toBeVisible();
    await message.press("Enter");
    const log = page.getByRole("log");
    await expect(log).toContainText(
      "Picture where you are standing during sunrise.",
    );
    await expect(log).not.toContainText(correctChoice);
  });
}

test("teach-back asks twice and links a missed point to the lesson", async ({
  page,
}) => {
  await page.route(`**/v1/lessons/${lesson}/test`, (route) =>
    route.fulfill({
      json: { course_id: course, questions: [], passed: true },
    }),
  );
  await page.route(`**/v1/lessons/${lesson}/manifest`, (route) =>
    route.fulfill({
      json: {
        lesson_id: lesson,
        course_id: course,
        title: "Earth and sunlight",
        version: 1,
        asset_origin: "https://example.test",
        slides: ["day", "night"].map((slide_id) => ({
          slide_id,
          html_url: `/v1/lessons/${lesson}/slides/${slide_id}`,
          duration_ms: 2000,
          narration_status: "disabled",
          narration_text: "",
          assets: [],
          checkpoints: [],
        })),
      },
    }),
  );
  await page.route(`**/v1/lessons/${lesson}/slides/*`, (route) =>
    route.fulfill({ json: { html: "<h1>Earth and sunlight</h1>" } }),
  );
  await page.route(`**/v1/lessons/${lesson}/position`, (route) =>
    route.fulfill({
      json:
        route.request().method() === "PUT"
          ? route.request().postDataJSON()
          : { version: 1, slide_id: "day", time_ms: 1000 },
    }),
  );
  const answers = [
    "The Earth turns.",
    "The sky looks different.",
    "The Sun is bright.",
  ];
  const replies = [
    { turn: 1, followUp: "Why does that bring daylight?" },
    { turn: 2, followUp: "What changes during the night?" },
    {
      turn: 3,
      summary: {
        covered: [{ text: "Earth's rotation", slideIndex: 0 }],
        missed: [{ text: "Night on the far side", slideIndex: 1 }],
      },
    },
  ];
  let turn = 0;
  await page.route(teachBack, (route) => {
    if (route.request().method() === "GET")
      return route.fulfill({ json: replies[turn - 1] ?? { turn: 0 } });
    expect(route.request().postDataJSON()).toEqual({
      turn,
      explanation: answers[turn],
    });
    return route.fulfill({ json: replies[turn++] });
  });

  await page.goto(testUrl);
  await expect(
    page.getByRole("link", { name: /back to your path/i }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Teach it back" }).click();
  for (const [index, answer] of answers.entries()) {
    await page.getByRole("textbox", { name: "Your explanation" }).fill(answer);
    await page.getByRole("button", { name: "Send explanation" }).click();
    if (index < 2)
      await expect(page.getByText(replies[index].followUp!)).toBeVisible();
  }
  await expect(page.getByText("Earth's rotation")).toBeVisible();
  const missed = page.getByRole("link", { name: "Night on the far side" });
  await expect(missed).toHaveAttribute("href", `/lesson/${lesson}?slide=1&t=0`);
  await page.reload();
  await page.getByRole("button", { name: "Teach it back" }).click();
  await expect(missed).toBeVisible();
  await expect(page.getByRole("textbox")).toHaveCount(0);
  expect(turn).toBe(3);
  await missed.click();
  await expect(page.getByText("Slide 2 of 2")).toBeVisible();
});
