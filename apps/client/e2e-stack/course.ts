// Course creation through the real UI, shared by F2-F4.
import type { Page } from "@playwright/test";
import { expect, type Flow } from "./stack";

export const topic = "Astronomy basics";

/**
 * Previews, outlines and lessons need check-in or paid credits. A new learner's first
 * daily check-in (shown as a toast on first open) covers course creation and the first
 * lesson, so course flows wait for it instead of granting credits.
 */
export async function awaitFirstCheckIn(
  page: Page,
  flow?: Flow,
): Promise<void> {
  await expect(
    page
      .getByRole("status")
      .filter({ hasText: /^\+\d[\d,]* Credits · daily check-in/ }),
  ).toBeVisible();
  await flow?.step("first daily check-in credits");
}

/**
 * Brief (guided chat) -> quoted preview -> sample outline -> confirm -> create.
 * Starts on /courses/create; ends on the new course's path page.
 */
export async function createCourse(page: Page, flow?: Flow): Promise<void> {
  await briefToPreviewQuote(page, flow);
  await previewToCourse(page, flow);
}

/** Guided-chat brief up to the quoted "Ready to preview?" step. Starts on /courses/create. */
export async function briefToPreviewQuote(
  page: Page,
  flow?: Flow,
): Promise<void> {
  await expect(
    page.getByRole("heading", { name: "Create a course", level: 1 }),
  ).toBeVisible();
  await flow?.step("create course start");

  // The brief is a scripted guided chat (no model call): answer the topic, skip the optional
  // questions, then confirm the summary card.
  await page
    .getByRole("textbox", { name: "What do you want to learn?" })
    .fill(topic);
  await page
    .locator(".gc-widget")
    .getByRole("button", { name: "Send", exact: true })
    .click();
  await flow?.step("topic answered in guided chat");
  for (let i = 0; i < 8; i += 1)
    await page.getByRole("button", { name: "Skip", exact: true }).click();
  await page.getByRole("button", { name: "Confirm", exact: true }).click();
  await expect(
    page.getByRole("status").filter({ hasText: "Brief confirmed" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Review preview cost" }).click();
  await expect(
    page.getByRole("heading", { name: "Ready to preview?" }),
  ).toBeVisible();
  await flow?.step("preview quote");
}

/** From the quoted preview step to the new course's path page. */
export async function previewToCourse(page: Page, flow?: Flow): Promise<void> {
  await page.getByRole("button", { name: /^Confirm & preview/ }).click();
  const preview = page.getByRole("heading", { name: "Course preview" });
  const failed = page
    .getByRole("alert")
    .filter({ hasText: "Preview unavailable" });
  await expect(preview.or(failed)).toBeVisible({ timeout: 120_000 });
  if (await failed.isVisible()) {
    await flow?.step("preview failed");
    // The owner-approved Hy4/GLM routing version (migration 20261123110000) is active on
    // every target, so a failed preview is a product or engine defect, never a skip.
    throw new Error(
      `course preview failed: ${await failed.innerText()}. The active approved pricing ` +
        "version must quote the route the engine serves; see the api and engine logs.",
    );
  }
  await flow?.step("course preview");

  await page.getByRole("button", { name: "Review sample outline" }).click();
  await page
    .getByRole("button", { name: "Confirm outline", exact: true })
    .click();
  await flow?.step("outline confirmed");
  await page.getByRole("button", { name: "Review creation cost" }).click();
  const create = page.getByRole("button", { name: /^Create outline/ });
  await expect(create).toBeEnabled();
  await flow?.step("creation cost");
  await create.click();
  await expect(page).toHaveURL(/\/course\/[^/]+$/, { timeout: 180_000 });
}
