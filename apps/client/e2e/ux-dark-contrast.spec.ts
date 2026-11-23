import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "./accounts-fixture";
import { apiBase } from "./servers";

test("settings checkboxes retain a usable label and native interaction", async ({
  page,
}) => {
  await page.goto("/settings");
  const checkbox = page.getByTestId("setting-auto-send");
  await expect(checkbox).toBeVisible();
  const label = checkbox.locator("xpath=ancestor::label");
  const bounds = await label.boundingBox();
  expect(bounds!.width).toBeGreaterThanOrEqual(44);
  expect(bounds!.height).toBeGreaterThanOrEqual(44);
  await expect(checkbox).toHaveCSS("border-radius", "50%");
  await checkbox.focus();
  await expect(checkbox).toHaveCSS("outline-style", "solid");
});

// Quick notes are audited separately. Dynamic detail routes need populated fixtures.
for (const path of [
  "/",
  "/courses",
  "/courses/create",
  "/marketplace",
  "/moderation",
  "/moderation/notices",
  "/profile",
  "/profile/setup",
  "/credits",
  "/credits/billing",
  "/more",
  "/friends",
  "/leagues",
  "/bookmarks",
  "/settings",
  "/settings/sessions",
  "/settings/characters",
  "/streak",
  "/analytics",
]) {
  test(`dark contrast: ${path}`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
    await page.goto(path);
    await expect(page.locator("main")).toBeVisible();
    if (path === "/settings/sessions")
      await expect(page.locator(".sessions-panel")).toHaveAttribute(
        "aria-busy",
        "false",
      );
    const results = await new AxeBuilder({ page })
      .withRules(["color-contrast"])
      .analyze();
    expect(results.violations).toEqual([]);
  });
}

test("dark contrast: populated lesson and transcript", async ({
  page,
  request,
}) => {
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
  await page.goto("/profile");
  const token = await page.evaluate(
    () =>
      JSON.parse(sessionStorage.getItem("duolify.native-store-fixture")!)
        .access_token as string,
  );
  const seeded = await request.post(`${apiBase}/test/player/fixture`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  expect(seeded.status()).toBe(201);
  const { lesson_id } = await seeded.json();
  await page.goto(`/lesson/${lesson_id}`);
  await expect(page.getByText("Slide 1 of 3")).toBeVisible();
  await page.getByRole("button", { name: "Transcript", exact: true }).click();
  await expect(page.getByRole("region", { name: "Transcript" })).toBeVisible();
  expect(
    (await new AxeBuilder({ page }).withRules(["color-contrast"]).analyze())
      .violations,
  ).toEqual([]);
});

test("dark contrast: lesson test question and answer error", async ({
  page,
}) => {
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
  await page.route("**/v1/features", (route) =>
    route.fulfill({ json: { lesson_test_tracer_enabled: true } }),
  );
  await page.route("**/v1/lessons/review161/test", (route) =>
    route.fulfill({
      json: {
        session_id: "opaque-session",
        questions: [
          {
            question_id: "item",
            topic_id: "topic",
            prompt: "Which answer?",
            choices: ["First", "Second"],
          },
        ],
        total_topics: 1,
        resolved_topics: 0,
      },
    }),
  );
  await page.route("**/v1/lessons/review161/test/answer", (route) =>
    route.fulfill({
      status: 500,
      json: { error: "Answer could not be saved" },
    }),
  );
  await page.goto("/lesson/review161/test");
  await page.getByLabel("First", { exact: true }).click();
  await page.getByRole("button", { name: "Check", exact: true }).click();
  await expect(page.getByText("Answer could not be saved")).toBeVisible();
  expect(
    (await new AxeBuilder({ page }).withRules(["color-contrast"]).analyze())
      .violations,
  ).toEqual([]);
});

test("dark contrast: populated course publication preview", async ({
  page,
  request,
}) => {
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
  await page.goto("/profile");
  const token = await page.evaluate(
    () =>
      JSON.parse(sessionStorage.getItem("duolify.native-store-fixture")!)
        .access_token as string,
  );
  const seeded = await request.post(`${apiBase}/test/sharing/course`, {
    headers: { Authorization: `Bearer ${token}` },
    data: { variant: "safe" },
  });
  expect(seeded.status()).toBe(201);
  const { course_id } = await seeded.json();
  await page.route(
    /\/v1\/(courses|features|gamification)(\/|$|\?)/,
    async (route) => {
      const original = route.request();
      const target = new URL(original.url());
      target.host = new URL(apiBase).host;
      target.protocol = new URL(apiBase).protocol;
      const headers = original.headers();
      delete headers.origin;
      await route.fulfill({
        response: await request.fetch(target.href, {
          method: original.method(),
          headers,
          data: original.postData() ?? undefined,
        }),
      });
    },
  );
  await page.goto(`/course/${course_id}`);
  await page.getByRole("button", { name: "Preview publication draft" }).click();
  await expect(page.getByTestId("draft-verdict")).toHaveText(
    "Passed screening",
  );
  expect(
    (await new AxeBuilder({ page }).withRules(["color-contrast"]).analyze())
      .violations,
  ).toEqual([]);
});

test("dark contrast: marketplace course and report dialog", async ({
  page,
  request,
}) => {
  test.setTimeout(90_000);
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
  const email = `contrast-${crypto.randomUUID()}@example.test`;
  await request.post(`${apiBase}/v1/auth/code`, {
    data: { email, age_confirmed: true },
  });
  const { code } = await (
    await request.get(`${apiBase}/test/mail?email=${email}`)
  ).json();
  const verified = await request.post(`${apiBase}/v1/auth/verify`, {
    data: { email, code, age_confirmed: true },
  });
  const headers = {
    Authorization: `Bearer ${(await verified.json()).access_token}`,
  };
  const seeded = await request.post(`${apiBase}/test/marketplace/course`, {
    headers,
    data: { title: "Contrast course", description: "Short lessons" },
  });
  expect(seeded.status()).toBe(201);
  const { course_id } = await seeded.json();
  const draft = await request.post(
    `${apiBase}/v1/courses/${course_id}/publication-drafts`,
    { headers, data: {} },
  );
  expect((await draft.json()).verdict).toBe("pass");
  expect(
    (
      await request.put(
        `${apiBase}/v1/courses/${course_id}/marketplace-listing`,
        { headers, data: { category: "tech", language: "en", tags: [] } },
      )
    ).status(),
  ).toBe(200);
  await page.route("**/v1/**", async (route) => {
    const original = route.request();
    const target = new URL(original.url());
    target.host = new URL(apiBase).host;
    const forwarded = original.headers();
    delete forwarded.origin;
    await route.fulfill({
      response: await request.fetch(target.href, {
        method: original.method(),
        headers: forwarded,
        data: original.postData() ?? undefined,
      }),
    });
  });
  await page.goto(`/marketplace/${course_id}`);
  await page.getByRole("button", { name: "More: report course" }).click();
  await page.getByRole("menuitem", { name: "Report this course" }).click();
  await expect(
    page.getByRole("dialog", { name: "Report this course" }),
  ).toBeVisible();
  expect(
    (
      await new AxeBuilder({ page })
        .exclude("iframe")
        .withRules(["color-contrast"])
        .analyze()
    ).violations,
  ).toEqual([]);
});

test("dark contrast: populated course cast", async ({ page, request }) => {
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
  await page.goto("/settings/characters");
  const token = await page.evaluate(
    () =>
      JSON.parse(sessionStorage.getItem("duolify.native-store-fixture")!)
        .access_token as string,
  );
  const headers = { Authorization: `Bearer ${token}` };
  const seeded = await request.post(`${apiBase}/test/characters`, { headers });
  expect(seeded.status()).toBe(200);
  const { course_id } = await seeded.json();
  await page.route(/\/v1\/(characters|courses\/[^/]+\/cast)/, async (route) => {
    const url = new URL(route.request().url());
    url.host = new URL(apiBase).host;
    await route.fulfill({
      response: await request.fetch(url.href, {
        method: route.request().method(),
        headers,
        data: route.request().postData() ?? undefined,
      }),
    });
  });
  await page.goto(`/courses/${course_id}/cast`);
  await expect(page.getByLabel("Main guide")).toBeVisible();
  expect(
    (
      await new AxeBuilder({ page })
        .exclude("iframe")
        .withRules(["color-contrast"])
        .analyze()
    ).violations,
  ).toEqual([]);
});

test("dark contrast: friend invite and blocked course", async ({ page }) => {
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
  await page.route("**/v1/social/friend-link/resolve", (route) =>
    route.fulfill({
      json: { username: "sam", display_name: "Sam", avatar: { initial: "S" } },
    }),
  );
  await page.goto(`/friends/invite/${"a".repeat(43)}`);
  await expect(
    page.getByRole("button", { name: "Send friend request" }),
  ).toBeVisible();
  expect(
    (await new AxeBuilder({ page }).withRules(["color-contrast"]).analyze())
      .violations,
  ).toEqual([]);
  await page.route("**/v1/courses/blocked-test", (route) =>
    route.fulfill({ json: { moderation_blocked: true } }),
  );
  await page.goto("/moderation/blocked/blocked-test");
  await expect(
    page.getByRole("heading", { name: "This course was removed" }),
  ).toBeVisible();
  expect(
    (await new AxeBuilder({ page }).withRules(["color-contrast"]).analyze())
      .violations,
  ).toEqual([]);
});
