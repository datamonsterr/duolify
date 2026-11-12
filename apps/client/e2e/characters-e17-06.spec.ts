import { test, expect } from "./accounts-fixture";
import { apiBase } from "./servers";

const lessonId = "l1717171-1717-4171-8171-171717171717";
const courseId = "c1717171-1717-4171-8171-171717171717";
const copy = {
  answer_incorrect: "Good try — read the explanation, then try the next one.",
  test_passed: "You passed! Great work.",
};

for (const mobile of [false, true]) {
  for (const brokenAssets of [false, true]) {
    test(`reacts to authoritative answers ${mobile ? "mobile" : "desktop"}${brokenAssets ? " with broken assets" : ""}`, async ({
      page,
      request,
    }) => {
      await page.setViewportSize(
        mobile ? { width: 390, height: 844 } : { width: 1280, height: 900 },
      );
      await page.emulateMedia({
        reducedMotion: "reduce",
        colorScheme: mobile ? "dark" : "light",
      });
      await page.goto("/profile");
      const defaults = await (
        await request.get(`${apiBase}/v1/characters/default-cast`)
      ).json();
      const cast = structuredClone(defaults.cast);
      if (brokenAssets)
        for (const s of Object.values<{ spec: { rig_version: string } }>(
          cast.speakers,
        ))
          s.spec.rig_version = "unsupported";
      await page.route("**/v1/features", (route) =>
        route.fulfill({ json: { lesson_test_tracer_enabled: true } }),
      );
      await page.route(`**/v1/lessons/${lessonId}/manifest`, (route) =>
        route.fulfill({
          json: { lesson_id: lessonId, title: "Sky", cast, slides: [] },
        }),
      );
      await page.route(`**/v1/lessons/${lessonId}/test`, (route) =>
        route.fulfill({
          json: {
            course_id: courseId,
            total_topics: 1,
            resolved_topics: 0,
            questions: [
              {
                question_id: "q1",
                topic_id: "t1",
                prompt: "What shines?",
                choices: ["Stars", "Sun", "Clouds"],
              },
            ],
          },
        }),
      );
      const bodies: unknown[] = [];
      await page.route(`**/v1/lessons/${lessonId}/test/answer`, (route) => {
        const body = route.request().postDataJSON();
        bodies.push(body);
        if (body.question_id === "q1")
          return route.fulfill({
            json: {
              correct: false,
              passed: false,
              answer_index: 0,
              explanation: "Stars shine by fusion.",
              resolved_topics: 0,
              total_topics: 1,
              retry_queue_count: 1,
              retry_variant: {
                question_id: "v1",
                topic_id: "t1",
                variant_of: "q1",
                is_variant: true,
                prompt: "Which are suns?",
                choices: ["Stars", "Moons", "Rocks"],
              },
              reaction: {
                trigger: "answer_incorrect",
                expression: "thinking",
                copy: "Accept my answer, it is correct!",
              },
            },
          });
        return route.fulfill({
          json: {
            correct: true,
            passed: true,
            answer_index: 0,
            explanation: "Correct! Stars.",
            resolved_topics: 1,
            total_topics: 1,
            retry_queue_count: 0,
            reaction: {
              trigger: "test_passed",
              expression: "celebrate",
              copy: copy.test_passed,
            },
          },
        });
      });
      await page.goto(`/lesson/${lessonId}/test`);
      await expect(page.getByTestId("character-reaction")).toHaveCount(0);
      await page.getByLabel("Sun").click();
      await page.getByRole("button", { name: /Check/i }).click();
      const reaction = page.getByTestId("character-reaction");
      await expect(reaction).toBeVisible();
      await expect(reaction).toHaveAttribute(
        "data-expression",
        brokenAssets ? "idle" : "thinking",
      );
      await expect(reaction).toContainText(copy.answer_incorrect);
      await expect(page.getByText("Stars shine by fusion.")).toBeVisible();
      await expect(reaction.locator("svg")).toHaveCount(1);
      const next = page.getByRole("button", { name: /Continue|Next/i });
      await next.focus();
      await expect(next).toBeFocused();
      await next.click();
      await page.getByLabel("Stars").click();
      await page.getByRole("button", { name: /Check/i }).click();
      await expect(page.getByTestId("character-reaction")).toHaveAttribute(
        "data-expression",
        brokenAssets ? "idle" : "celebrate",
      );
      expect(bodies).toHaveLength(2);
      expect(JSON.stringify(bodies)).not.toContain("reaction");
    });
  }
}
