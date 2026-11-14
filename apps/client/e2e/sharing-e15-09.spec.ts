import type { APIRequestContext, Page } from "@playwright/test";
import { apiBase } from "./servers";
import { test, expect } from "./accounts-fixture";

type Headers = { Authorization: string };

async function account(request: APIRequestContext): Promise<Headers> {
  const email = `reporter-${crypto.randomUUID()}@example.test`;
  await request.post(`${apiBase}/v1/auth/code`, {
    data: { email, age_confirmed: true },
  });
  const { code } = await (
    await request.get(`${apiBase}/test/mail?email=${email}`)
  ).json();
  const verified = await request.post(`${apiBase}/v1/auth/verify`, {
    data: { email, code, age_confirmed: true },
  });
  expect(verified.status()).toBe(200);
  return {
    Authorization: `Bearer ${(await verified.json()).access_token}`,
  };
}

async function listed(request: APIRequestContext, title: string) {
  const headers = await account(request);
  const seeded = await request.post(`${apiBase}/test/marketplace/course`, {
    headers,
    data: { title, description: "Short lessons" },
  });
  expect(seeded.status()).toBe(201);
  const course = (await seeded.json()).course_id as string;
  const draft = await request.post(
    `${apiBase}/v1/courses/${course}/publication-drafts`,
    { headers, data: {} },
  );
  expect((await draft.json()).verdict).toBe("pass");
  const put = await request.put(
    `${apiBase}/v1/courses/${course}/marketplace-listing`,
    { headers, data: { category: "tech", language: "en", tags: [] } },
  );
  expect(put.status()).toBe(200);
  return course;
}

async function forward(page: Page, request: APIRequestContext) {
  await page.route("**/v1/**", async (route) => {
    const original = route.request();
    const target = new URL(original.url());
    target.host = new URL(apiBase).host;
    const headers = original.headers();
    delete headers.origin;
    await route.fulfill({
      response: await request.fetch(target.href, {
        method: original.method(),
        headers,
        data: original.postData() ?? undefined,
      }),
    });
  });
}

const inCatalogue = async (request: APIRequestContext, course: string) => {
  const res = await request.get(
    `${apiBase}/v1/marketplace/courses?sort=new&q=${encodeURIComponent("Reported")}`,
  );
  expect(res.status()).toBe(200);
  return (await res.json()).items.some(
    (i: { course_id: string }) => i.course_id === course,
  );
};

for (const mobile of [false, true]) {
  test.describe(mobile ? "touch phone" : "desktop", () => {
    test.use({
      hasTouch: mobile,
      viewport: mobile
        ? { width: 390, height: 844 }
        : { width: 1280, height: 800 },
    });
    test(`three distinct reporters delist a course at once (${mobile ? "mobile" : "desktop"})`, async ({
      page,
      request,
    }) => {
      const course = await listed(request, "Reported course");
      let auth = "";
      await forward(page, request);
      await page.route("**/v1/**", async (route) => {
        auth = route.request().headers().authorization || auth;
        await route.fallback();
      });
      expect(await inCatalogue(request, course)).toBe(true);
      await page.goto(`/marketplace/${course}`);
      await page.getByRole("button", { name: "More: report course" }).click();
      await page.getByRole("menuitem", { name: "Report this course" }).click();
      const sheet = page.getByRole("dialog", { name: "Report this course" });
      await expect(sheet).toBeVisible();
      await sheet.getByLabel("Copyright").check();
      await sheet
        .getByLabel("Details, optional")
        .fill("Lesson 4 copies a textbook chapter.");
      await sheet.getByRole("button", { name: "Send report" }).click();
      await expect(sheet.getByRole("status")).toContainText(
        "Thanks — report sent",
      );
      expect(await inCatalogue(request, course)).toBe(true);
      // A duplicate from the same learner merges and never counts twice.
      const mine = { Authorization: auth };
      const dup = await request.post(`${apiBase}/v1/marketplace/reports`, {
        headers: mine,
        data: { target_type: "course", target_id: course, reason: "spam" },
      });
      expect(dup.status()).toBe(200);
      expect((await dup.json()).merged).toBe(true);
      // Two more distinct learners reach the threshold of three.
      for (const reason of ["spam", "misleading"]) {
        const res = await request.post(`${apiBase}/v1/marketplace/reports`, {
          headers: await account(request),
          data: { target_type: "course", target_id: course, reason },
        });
        expect(res.status()).toBe(202);
        expect(await res.json()).toEqual({ status: "received", merged: false });
      }
      expect(await inCatalogue(request, course)).toBe(false);
      await page.reload();
      await expect(
        page.getByRole("heading", { name: "This course isn't available" }),
      ).toBeVisible();
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth - window.innerWidth,
        ),
      ).toBeLessThanOrEqual(0);
    });
  });
}

test("report sheet states: keyboard, offline retry, rate limit, dark and reduced motion", async ({
  page,
  request,
}) => {
  const course = await listed(request, "Reported states");
  await forward(page, request);
  await page.setViewportSize({ width: 390, height: 844 });
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
  await page.goto(`/marketplace/${course}`);
  const more = page.getByRole("button", { name: "More: report course" });
  await more.click();
  await page.getByRole("menuitem", { name: "Report this course" }).click();
  const sheet = page.getByRole("dialog", { name: "Report this course" });
  await expect(sheet.getByLabel("Unsafe instructions")).toBeFocused();
  await expect(
    sheet.getByRole("button", { name: "Send report" }),
  ).toBeDisabled();
  await sheet.getByLabel("Other").check();
  const long = "Needs attention 界 ".repeat(25);
  await sheet.getByLabel("Details, optional").fill(long);
  let calls = 0;
  await page.route(`**/v1/marketplace/reports`, async (route) => {
    calls += 1;
    if (calls === 1) return route.abort();
    if (calls === 2)
      return route.fulfill({
        status: 429,
        headers: { "Retry-After": "60" },
        json: { error: "rate_limited" },
      });
    return route.fallback();
  });
  await sheet.getByRole("button", { name: "Send report" }).click();
  await expect(sheet.getByRole("alert")).toContainText(
    "Couldn't reach Duolify",
  );
  await expect(sheet.getByLabel("Details, optional")).toHaveValue(long);
  await sheet.getByRole("button", { name: "Retry", exact: true }).click();
  await expect(sheet.getByRole("alert")).toContainText("Too many");
  await sheet.getByRole("button", { name: "Retry", exact: true }).click();
  await expect(sheet.getByRole("status")).toContainText("Thanks — report sent");
  expect(await sheet.evaluate((el) => getComputedStyle(el).animationName)).toBe(
    "none",
  );
  expect(
    await page.evaluate(() => getComputedStyle(document.body).backgroundColor),
  ).not.toBe("rgb(255, 255, 255)");
  await page.keyboard.press("Escape");
  await expect(sheet).toBeHidden();
  await expect(more).toBeFocused();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth - window.innerWidth,
    ),
  ).toBeLessThanOrEqual(0);
});
