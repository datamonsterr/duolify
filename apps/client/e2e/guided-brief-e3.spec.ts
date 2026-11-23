import { test, expect } from "./accounts-fixture";
import type { Page } from "@playwright/test";

const send = (page: Page) =>
  page
    .locator(".gc-widget")
    .getByRole("button", { name: "Send", exact: true })
    .click();

test.beforeEach(async ({ page }) => {
  await page.route("**/v1/course-brief/config", (r) =>
    r.fulfill({
      json: { enabled: false, credits: null, pricing_version: "v" },
    }),
  );
});

for (const [width, height] of [
  [390, 844],
  [1280, 800],
] as const) {
  test(`course brief as a chat at ${width}px: confirmed revision, no preview or Credits calls`, async ({
    page,
  }, info) => {
    const calls: string[] = [];
    let confirmed: unknown;
    await page.route("**/v1/course-brief/confirm", async (route) => {
      confirmed = route.request().postDataJSON();
      calls.push("confirm");
      await route.fulfill({
        json: { id: "rev-1", content_hash: "h", brief: { topic: "Sky" } },
      });
    });
    page.on("request", (r) => {
      if (
        /\/v1\/(course-brief\/chat|credits|pricing|courses\/preview)/.test(
          r.url(),
        )
      )
        calls.push(new URL(r.url()).pathname);
    });
    await page.setViewportSize({ width, height });
    await page.goto("/courses/create");
    await expect(
      page.getByText("What do you want to learn?").first(),
    ).toBeVisible();
    await expect(page.getByLabel("Topic", { exact: true })).toHaveCount(0);
    await page.screenshot({ path: info.outputPath(`${width}-start.png`) });
    await page
      .getByRole("textbox", { name: "What do you want to learn?" })
      .fill("Sky watching");
    await send(page);
    await page
      .getByRole("textbox", { name: "What do you want from it?" })
      .fill("Explain it to my kids");
    await send(page);
    await page.getByRole("button", { name: "Skip", exact: true }).click(); // include
    await page.getByRole("button", { name: "Skip", exact: true }).click(); // skip topics
    await page.getByRole("radio", { name: "Beginner" }).check();
    await send(page);
    await page.getByRole("radio", { name: "Light" }).check();
    await send(page);
    await page.getByRole("radio", { name: "At home" }).check();
    await send(page);
    await page.getByRole("radio", { name: "10 minutes" }).check();
    await send(page);
    await page.getByRole("radio", { name: "English" }).check();
    await send(page);
    await expect(
      page.getByText("Ready to confirm your brief?").first(),
    ).toBeVisible();
    await page.screenshot({ path: info.outputPath(`${width}-confirm.png`) });
    await page.getByRole("button", { name: "Confirm", exact: true }).click();
    await expect(
      page.getByRole("status").filter({ hasText: "Brief confirmed" }),
    ).toBeVisible();
    expect(confirmed).toMatchObject({
      confirmed: true,
      brief: {
        topic: "Sky watching",
        goal: "Explain it to my kids",
        level: "Beginner",
        maths: "Light maths",
        lesson_length: "10 minutes",
        language: "English",
        include_topics: "",
      },
    });
    // Only the confirmation was called: no brief chat model call, no Credits side effects.
    expect(calls).toEqual(["confirm"]);
  });
}

test("keyboard only: topic, skip everything, confirm", async ({ page }) => {
  await page.route("**/v1/course-brief/confirm", (r) =>
    r.fulfill({
      json: { id: "rev-2", content_hash: "h", brief: { topic: "Sky" } },
    }),
  );
  await page.goto("/courses/create");
  await page
    .getByRole("textbox", { name: "What do you want to learn?" })
    .fill("Sky");
  await page.keyboard.press("Enter");
  for (let i = 0; i < 8; i += 1) {
    await page.getByRole("button", { name: "Skip", exact: true }).focus();
    await page.keyboard.press("Enter");
  }
  await page.getByRole("button", { name: "Confirm", exact: true }).focus();
  await page.keyboard.press("Enter");
  await expect(
    page.getByRole("status").filter({ hasText: "Brief confirmed" }),
  ).toBeVisible();
});

test("resume: reload keeps the answered bubbles", async ({ page }) => {
  await page.goto("/courses/create");
  await page
    .getByRole("textbox", { name: "What do you want to learn?" })
    .fill("Sky");
  await send(page);
  await page.reload();
  await expect(
    page.getByRole("button", { name: "Change answer: Sky" }),
  ).toBeVisible();
  await expect(
    page.getByText("What do you want from it?").first(),
  ).toBeVisible();
});
