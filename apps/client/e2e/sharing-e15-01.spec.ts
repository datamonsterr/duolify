import type { APIRequestContext, Page } from "@playwright/test";
import { apiBase } from "./servers";
import { test, expect } from "./accounts-fixture";

type Variant = "safe" | "unsafe" | "private" | "pii" | "down";
// Marker the API seeds into notes/chat/answers for the "private" variant.
const PRIVATE_MARKER = "E15-PRIVATE-MARKER";

async function tokenOf(page: Page) {
  await page.goto("/profile");
  const tokens = await page.evaluate(() =>
    JSON.parse(sessionStorage.getItem("duolify.native-store-fixture")!),
  );
  return { Authorization: `Bearer ${tokens.access_token}` };
}

async function seed(
  request: APIRequestContext,
  headers: { Authorization: string },
  variant: Variant,
) {
  const response = await request.post(`${apiBase}/test/sharing/course`, {
    headers,
    data: { variant },
  });
  expect(response.status()).toBe(201);
  return (await response.json()).course_id as string;
}

// The browser's course, feature and XP calls are forwarded to the E2E API server.
async function proxyCourses(page: Page, request: APIRequestContext) {
  await page.route(
    /\/v1\/(courses|features|gamification)(\/|$|\?)/,
    async (route) => {
      const original = route.request();
      const target = new URL(original.url());
      const headers = original.headers();
      delete headers.origin;
      target.host = new URL(apiBase).host;
      target.protocol = new URL(apiBase).protocol;
      await route.fulfill({
        response: await request.fetch(target.href, {
          method: original.method(),
          headers,
          data: original.postData() ?? undefined,
        }),
      });
    },
  );
}

async function open(page: Page, course: string) {
  await page.goto(`/course/${course}`);
  const trigger = page.getByRole("button", {
    name: "Preview publication draft",
  });
  await trigger.click();
  const dialog = page.getByRole("dialog", {
    name: "Preview publication draft",
  });
  await expect(dialog).toBeVisible();
  return { dialog, trigger };
}

for (const [label, size] of [
  ["desktop", { width: 1280, height: 900 }],
  ["mobile", { width: 390, height: 844 }],
] as const) {
  test.describe(`${label} publication draft`, () => {
    test.use({ viewport: size, hasTouch: label === "mobile" });

    test("a safe draft passes with outline, narration note and no public link", async ({
      page,
      request,
    }) => {
      await page.emulateMedia({
        reducedMotion: "reduce",
        colorScheme: label === "mobile" ? "dark" : "light",
      });
      const headers = await tokenOf(page);
      const course = await seed(request, headers, "safe");
      await proxyCourses(page, request);
      const { dialog } = await open(page, course);
      await expect(dialog.getByTestId("draft-verdict")).toHaveText(
        "Passed screening",
      );
      await expect(dialog.getByRole("list", { name: "Outline" })).toBeVisible();
      await expect(dialog).toContainText("Narration audio is not included");
      await expect(
        dialog.getByRole("button", {
          name: "Public sharing is not available yet",
        }),
      ).toBeDisabled();
      await expect(dialog.getByText(/copy link/i)).toHaveCount(0);
      const box = await dialog.boundingBox();
      if (label === "mobile")
        expect(box!.width).toBeGreaterThanOrEqual(size.width - 1);
      else expect(box!.width).toBeLessThan(size.width);
    });

    test("an unsafe draft is blocked", async ({ page, request }) => {
      const headers = await tokenOf(page);
      const course = await seed(request, headers, "unsafe");
      await proxyCourses(page, request);
      const { dialog } = await open(page, course);
      await expect(dialog.getByTestId("draft-verdict")).toHaveText("Blocked");
    });

    test("an email in a slide needs review and shows only the location", async ({
      page,
      request,
    }) => {
      const headers = await tokenOf(page);
      const course = await seed(request, headers, "pii");
      await proxyCourses(page, request);
      const { dialog } = await open(page, course);
      await expect(dialog.getByTestId("draft-verdict")).toHaveText(
        "Needs review",
      );
      const findings = dialog.getByRole("list", { name: "Findings" });
      await expect(findings).toContainText("Personal data");
      await expect(findings).toContainText(/lesson \d+ · slide \d+/);
      await expect(dialog).not.toContainText("@");
    });

    test("private learner data never reaches the draft", async ({
      page,
      request,
    }) => {
      const headers = await tokenOf(page);
      const course = await seed(request, headers, "private");
      await proxyCourses(page, request);
      const { dialog } = await open(page, course);
      await expect(dialog.getByTestId("draft-verdict")).toBeVisible();
      await expect(dialog).not.toContainText(PRIVATE_MARKER);
      const raw = await (
        await request.post(
          `${apiBase}/v1/courses/${course}/publication-drafts`,
          {
            headers,
            data: {},
          },
        )
      ).text();
      expect(raw).not.toContain(PRIVATE_MARKER);
    });

    test("screening down fails closed, keeps learning usable and offers retry", async ({
      page,
      request,
    }) => {
      const headers = await tokenOf(page);
      const course = await seed(request, headers, "down");
      await proxyCourses(page, request);
      const { dialog } = await open(page, course);
      await expect(dialog.getByTestId("draft-verdict")).toHaveText(
        "Screening pending",
      );
      await expect(dialog).toContainText("Learning is not affected");
      await dialog.getByRole("button", { name: "Retry screening" }).click();
      await expect(dialog.getByTestId("draft-verdict")).toHaveText(
        "Screening pending",
      );
      await dialog.getByRole("button", { name: "Close" }).click();
      await expect(dialog).toBeHidden();
      // Private learning is untouched.
      await expect(
        page.getByRole("link", { name: "Course Cast" }),
      ).toBeVisible();
    });

    test("offline build shows an error and recovers", async ({
      page,
      request,
    }) => {
      const headers = await tokenOf(page);
      const course = await seed(request, headers, "safe");
      await proxyCourses(page, request);
      await page.goto(`/course/${course}`);
      await page.route("**/publication-drafts", (route) => route.abort(), {
        times: 1,
      });
      await page
        .getByRole("button", { name: "Preview publication draft" })
        .click();
      const dialog = page.getByRole("dialog");
      await expect(dialog).toContainText("Couldn't reach Duolify");
      await dialog.getByRole("button", { name: "Try again" }).click();
      await expect(dialog.getByTestId("draft-verdict")).toBeVisible();
    });
  });
}

test("keyboard: focus moves into the dialog, stays inside and returns on Escape", async ({
  page,
  request,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  const headers = await tokenOf(page);
  const course = await seed(request, headers, "safe");
  await proxyCourses(page, request);
  await page.goto(`/course/${course}`);
  const trigger = page.getByRole("button", {
    name: "Preview publication draft",
  });
  await trigger.focus();
  await page.keyboard.press("Enter");
  const dialog = page.getByRole("dialog", {
    name: "Preview publication draft",
  });
  await expect(dialog.getByRole("button", { name: "Close" })).toBeFocused();
  for (let i = 0; i < 6; i++) {
    await page.keyboard.press("Tab");
    expect(
      await page.evaluate(() => !!document.activeElement?.closest("dialog")),
    ).toBe(true);
  }
  await page.keyboard.press("Escape");
  await expect(dialog).toBeHidden();
  await expect(trigger).toBeFocused();
});

test("owner-only: another learner gets the neutral refusal and publishing is unavailable", async ({
  page,
  request,
}) => {
  const owner = await tokenOf(page);
  const course = await seed(request, owner, "safe");
  const built = await request.post(
    `${apiBase}/v1/courses/${course}/publication-drafts`,
    { headers: owner, data: {} },
  );
  expect(built.status()).toBe(201);
  const draft = await built.json();

  const email = `e15-01-${crypto.randomUUID()}@example.test`;
  await request.post(`${apiBase}/v1/auth/code`, {
    data: { email, age_confirmed: true },
  });
  const { code } = await (
    await request.get(`${apiBase}/test/mail?email=${email}`)
  ).json();
  const verified = await request.post(`${apiBase}/v1/auth/verify`, {
    data: { email, code, age_confirmed: true },
  });
  const other = {
    Authorization: `Bearer ${(await verified.json()).access_token}`,
  };
  const base = `${apiBase}/v1/courses/${course}`;
  const foreignRead = await request.get(
    `${base}/publication-drafts/${draft.id}`,
    { headers: other },
  );
  const unknownRead = await request.get(
    `${base}/publication-drafts/00000000-0000-4000-8000-000000000000`,
    { headers: other },
  );
  expect(foreignRead.status()).toBe(404);
  expect(unknownRead.status()).toBe(404);
  expect(await foreignRead.json()).toEqual(await unknownRead.json());
  expect(
    (
      await request.post(`${base}/publish`, { headers: other, data: {} })
    ).status(),
  ).toBe(404);
  const publish = await request.post(`${base}/publish`, {
    headers: owner,
    data: {},
  });
  expect(publish.status()).toBe(409);
  expect(await publish.json()).toMatchObject({
    error: "visibility_unavailable",
  });
});
