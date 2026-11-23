import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "@playwright/test";
import type { Page } from "@playwright/test";

const url = (id: string) => `/guided-chat.html?fixture=script&id=${id}`;
const audit = async (page: Page, where: string) => {
  const { violations } = await new AxeBuilder({ page })
    .include(".gc")
    .analyze();
  expect(
    violations.map((v) => `${v.id}: ${v.nodes[0]?.html}`),
    where,
  ).toEqual([]);
};

test("every widget passes axe and a keyboard-only run completes the script", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(url("fixture.tour"));
  const k = page.keyboard;
  // text
  await audit(page, "text");
  await page.getByRole("textbox", { name: "What should I call you?" }).focus();
  await k.type("Sam");
  await k.press("Enter");
  // single: arrows move within the radio group, focus lands on the first control
  await expect(page.getByRole("radio", { name: "Beginner" })).toBeFocused();
  await audit(page, "single");
  await k.press("ArrowDown");
  await expect(page.getByRole("radio", { name: "Some" })).toBeFocused();
  await k.press("Enter"); // Enter confirms through the hidden Send
  // multi
  await expect(page.getByRole("checkbox", { name: "Speaking" })).toBeFocused();
  await audit(page, "multi");
  await k.press("Space");
  await k.press("Enter");
  // toggle
  await audit(page, "toggle");
  await expect(page.getByRole("radio", { name: "Yes" })).toBeFocused();
  await k.press("Space");
  await k.press("Enter");
  // time
  await audit(page, "time");
  await page.getByLabel("What time suits you?").focus();
  await k.type("0730AM");
  await k.press("Enter");
  // date
  await audit(page, "date");
  await page.getByLabel("When do you want to start?").fill("2026-12-01");
  await k.press("Enter");
  // number stepper
  await audit(page, "number");
  await page.getByRole("button", { name: "More" }).focus();
  await k.press("Enter");
  await page.getByRole("spinbutton").focus();
  await k.press("Enter");
  // long text
  await audit(page, "longtext");
  await page.getByRole("button", { name: "Skip", exact: true }).focus();
  await k.press("Enter");
  // file
  await audit(page, "file");
  await page.getByRole("button", { name: "Skip", exact: true }).focus();
  await k.press("Enter");
  // rating
  await audit(page, "rating");
  await page.getByRole("radio", { name: "5 stars" }).focus();
  await k.press("Space");
  await k.press("Enter");
  // confirm
  await audit(page, "confirm");
  await page.getByRole("button", { name: "Confirm" }).focus();
  await k.press("Enter");
  await expect(page.getByRole("status")).toHaveText("Done.");
  await audit(page, "done");
});

test("secure widgets pass axe", async ({ page }) => {
  await page.goto(url("fixture.secure"));
  await audit(page, "email");
  await page.getByLabel("What's your email?").fill("a@b.co");
  await page.keyboard.press("Enter");
  await audit(page, "code");
  await page.getByLabel("Enter the code we sent.").fill("123456");
  await page.keyboard.press("Enter");
  await audit(page, "password");
  await page.getByLabel("Type your password.").fill("x");
  await page.keyboard.press("Enter");
  await audit(page, "destructive confirm");
});

test("the polite log announces each question once and reduced motion drops the bar transition", async ({
  page,
}) => {
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.goto(url("fixture.tour"));
  const log = page.getByRole("log", { name: "New messages" });
  await expect(log).toHaveAttribute("aria-live", "polite");
  await page
    .getByRole("textbox", { name: "What should I call you?" })
    .fill("Sam");
  await page.keyboard.press("Enter");
  await expect(log.locator("p")).toHaveCount(2);
  await page.getByRole("button", { name: "Change answer: Sam" }).click();
  await page.keyboard.press("Escape");
  await expect(log.locator("p").filter({ hasText: "level" })).toHaveCount(1);
  await expect(log.locator("p")).toHaveCount(3);
  const transition = await page
    .locator(".gc-bar")
    .evaluate((el) => getComputedStyle(el).transitionDuration);
  expect(transition).toBe("0s");
});
