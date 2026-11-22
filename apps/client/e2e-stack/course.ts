// Course creation through the real UI, shared by F2-F4.
import type { Page } from "@playwright/test";
import { expect, grantCredits, test, type Flow } from "./stack";

export const topic = "Astronomy basics";

/** Paid credits are required for previews and outlines; real payments are off. */
export async function fundLearner(learnerId: string): Promise<void> {
  const granted = await grantCredits(learnerId, 500);
  test.fixme(
    !granted,
    "Course creation needs purchased credits and card payments are disabled (STRIPE_MODE); " +
      "set E2E_STACK_GRANT_COMMAND to fund the learner on a remote stack.",
  );
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

  await page.getByRole("textbox", { name: "Your answer" }).fill(topic);
  await page.getByRole("button", { name: "Send answer", exact: true }).click();
  // The mock engine asks for the goal next; a live model may ask a follow-up instead.
  await expect(
    page.getByRole("log", { name: "New assistant messages" }).locator("p"),
  ).toHaveCount(2);
  await flow?.step("topic answered in guided chat");
  // Every brief line is editable; make sure the topic is set whatever the chat did.
  const brief = page.getByRole("complementary", { name: "Course brief" });
  if (
    !(await brief
      .getByRole("textbox", { name: "Topic", exact: true })
      .inputValue())
  )
    await brief
      .getByRole("textbox", { name: "Topic", exact: true })
      .fill(topic);

  await page
    .getByRole("button", { name: "Confirm brief", exact: true })
    .click();
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
    // The suite never approves pricing; the target's active version must quote the engine route.
    test.fixme(
      true,
      "pricing not approved on target: course preview failed. The active approved pricing " +
        "version must quote the route the engine serves (the owner-approved Hy4/GLM version " +
        "lands with PR #304, migration 20261123110000_hy4_routing_pricing_approved.sql).",
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
