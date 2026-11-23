import { expect, test } from "@playwright/test";
import type { Page } from "@playwright/test";

const url = (id: string, extra = "") =>
  `/guided-chat.html?fixture=script&id=${id}${extra}`;
const send = (page: Page) =>
  page
    .locator(".gc-widget")
    .getByRole("button", { name: "Send", exact: true })
    .click();

test("a fixture script answers with every widget, edits a bubble and resumes after reload", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(url("fixture.tour"));
  const log = page.getByRole("log", { name: "New messages" });
  await expect(log).toContainText("What should I call you?");
  await page
    .getByRole("textbox", { name: "What should I call you?" })
    .fill("Sam");
  await send(page);
  await expect(page.getByRole("radio", { name: "Beginner" })).toBeFocused();
  await page.getByRole("radio", { name: "Some" }).check();
  await send(page);
  await page.getByRole("checkbox", { name: "Speaking" }).check();
  await page.getByRole("checkbox", { name: "Reading" }).check();
  await send(page);
  await page.getByRole("radio", { name: "Yes" }).check();
  await send(page);
  await page.getByLabel("What time suits you?").fill("07:30");
  await send(page);
  await page.getByLabel("When do you want to start?").fill("2026-12-01");
  await send(page);
  await page.getByRole("button", { name: "More" }).click();
  await send(page);
  await page
    .getByRole("textbox", { name: "Anything else I should know?" })
    .fill("Short sessions");
  await send(page);
  await page.getByRole("button", { name: "Skip", exact: true }).first().click(); // photo
  await page.getByRole("radio", { name: "4 stars" }).check();
  await send(page);
  await expect(page.getByText("Ready to save these?").first()).toBeVisible();
  await expect(page.locator(".gc-summary")).toContainText("Sam");
  // Edit an earlier bubble: later answers stay.
  await page.getByRole("button", { name: "Change answer: Sam" }).click();
  await page
    .getByRole("textbox", { name: "What should I call you?" })
    .fill("Alex");
  await page.keyboard.press("Enter");
  await expect(page.locator(".gc-summary")).toContainText("Alex");
  await expect(page.locator(".gc-summary")).toContainText("07:30");
  // Reload resumes: the answered bubbles come back and the guide asks the confirm card.
  await page.reload();
  await expect(
    page.getByRole("button", { name: "Change answer: Alex" }),
  ).toBeVisible();
  await expect(page.getByText("Ready to save these?").first()).toBeVisible();
  await page.getByRole("button", { name: "Confirm" }).click();
  await expect(page.getByRole("status")).toHaveText("Done.");
  const submitted = await page.evaluate(() => window.__guided?.submitted[0]);
  expect(submitted).toMatchObject({
    name: "Alex",
    level: "some",
    time: "07:30",
    rating: 4,
  });
  await page.screenshot({
    path: process.env.GUIDED_SHOTS
      ? `${process.env.GUIDED_SHOTS}/tour-done-390.png`
      : undefined,
  });
});

test("Esc cancels an edit and free text is an answer when it validates", async ({
  page,
}) => {
  await page.goto(url("fixture.tour"));
  await page
    .getByRole("textbox", { name: "What should I call you?" })
    .fill("Sam");
  await send(page);
  await page.getByRole("button", { name: "Change answer: Sam" }).click();
  await page.keyboard.press("Escape");
  await expect(
    page.getByRole("button", { name: "Change answer: Sam" }),
  ).toBeVisible();
  await page.getByRole("radio", { name: "Beginner" }).check();
  await send(page);
  await page.getByRole("checkbox", { name: "Speaking" }).check();
  await send(page);
  await page.getByRole("radio", { name: "Yes" }).check();
  await send(page);
  await page.getByPlaceholder("Or type a message").fill("7:30");
  await page.keyboard.press("Enter");
  await expect(
    page.getByRole("button", { name: "Change answer: 07:30" }),
  ).toBeVisible();
  await page.getByPlaceholder("Or type a message").fill("what is this");
  await page.keyboard.press("Enter");
  await expect(page.getByRole("button", { name: "Use answer" })).toBeVisible();
});

test("secure inputs are real labelled inputs and never reach the draft", async ({
  page,
}) => {
  await page.goto(url("fixture.secure"));
  const email = page.getByLabel("What's your email?");
  await expect(email).toHaveAttribute("type", "email");
  await expect(email).toHaveAttribute("autocomplete", "email");
  await expect(page.getByPlaceholder("Or type a message")).toHaveCount(0);
  await email.fill("a@b.co");
  await send(page);
  const code = page.getByLabel("Enter the code we sent.");
  await expect(code).toHaveAttribute("autocomplete", "one-time-code");
  await code.fill("123456");
  await send(page);
  const pw = page.getByLabel("Enter your password to continue.");
  await expect(pw).toHaveAttribute("type", "password");
  await expect(pw).toHaveAttribute("autocomplete", "current-password");
  await pw.fill("hunter2");
  await send(page);
  // Destructive card: Cancel has focus, typed text cannot confirm.
  await expect(page.getByRole("button", { name: "Cancel" })).toBeFocused();
  const drafts = JSON.stringify(
    await page.evaluate(() => window.__guided?.drafts),
  );
  expect(drafts).not.toContain("hunter2");
  expect(drafts).not.toContain("123456");
  expect(drafts).not.toContain("a@b.co");
});

test("Arabic renders right to left", async ({ page }) => {
  await page.goto(url("fixture.tour", "&locale=ar"));
  await expect(page.locator("html")).toHaveAttribute("dir", "rtl");
  await expect(page.getByText("بماذا أناديك؟").first()).toBeVisible();
});

test("offline keeps answers and shows the sleepy state on a network step", async ({
  page,
  context,
}) => {
  await page.goto(url("fixture.tour"));
  await page
    .getByRole("textbox", { name: "What should I call you?" })
    .fill("Sam");
  await context.setOffline(true);
  await send(page);
  await expect(page.getByText("You're offline")).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Change answer: Sam" }),
  ).toBeVisible();
});
