import { test, expect } from "./accounts-fixture";
import type { Page } from "@playwright/test";

const send = (page: Page) =>
  page
    .locator(".gc-widget")
    .getByRole("button", { name: "Send", exact: true })
    .click();

for (const [width, height] of [
  [390, 844],
  [1280, 800],
] as const) {
  test(`onboarding as a chat at ${width}px: pointer path, resume, edit, no form`, async ({
    page,
  }, info) => {
    await page.setViewportSize({ width, height });
    await page.goto("/profile/setup");
    await expect(
      page.getByText("What do you want to learn?").first(),
    ).toBeVisible();
    // No multi-field form: exactly one widget at a time.
    await expect(page.locator(".gc-widget")).toHaveCount(1);
    await page.screenshot({ path: info.outputPath(`${width}-goal.png`) });
    await page.getByRole("radio", { name: "For fun" }).check();
    await send(page);
    await page.getByRole("radio", { name: "Some" }).check();
    await send(page);
    // Reload mid-chat resumes with the three answers' bubbles.
    await page.reload();
    await expect(
      page.getByRole("button", { name: "Change answer: For fun" }),
    ).toBeVisible();
    await expect(
      page.getByText("How many minutes a day?").first(),
    ).toBeVisible();
    await page.getByRole("button", { name: "More" }).click();
    await send(page);
    await page.getByRole("radio", { name: "Yes" }).check();
    await send(page);
    await page.getByLabel("What time?").fill("08:30");
    await send(page);
    await expect(page.getByText("Save these?").first()).toBeVisible();
    await page.screenshot({ path: info.outputPath(`${width}-confirm.png`) });
    // Edit a bubble, then confirm.
    await page.getByRole("button", { name: "Change answer: Some" }).click();
    await page.getByRole("radio", { name: "Expert" }).check();
    await send(page);
    await page.getByRole("button", { name: "Confirm" }).click();
    await expect(page.getByText("Setup complete")).toBeVisible();
    await page.goto("/profile");
    await expect(page.getByLabel("Goal", { exact: true })).toHaveValue(
      "For fun",
    );
    await expect(page.getByLabel("Reminder time")).toHaveValue("08:30");
  });
}

test("keyboard only: skip through and confirm", async ({ page }) => {
  await page.goto("/profile/setup");
  for (let i = 0; i < 3; i += 1)
    await page.getByRole("button", { name: "Skip", exact: true }).click();
  await page.getByRole("radio", { name: "No" }).focus();
  await page.keyboard.press("Space");
  await page.keyboard.press("Tab");
  await page.keyboard.press("Enter");
  await expect(page.getByText("Save these?").first()).toBeVisible();
  await page.getByRole("button", { name: "Confirm" }).focus();
  await page.keyboard.press("Enter");
  await expect(page.getByText("Setup complete")).toBeVisible();
});

test("offline: answers stay and the guide says so", async ({
  page,
  context,
}) => {
  await page.goto("/profile/setup");
  await page.getByRole("radio", { name: "For fun" }).check();
  await context.setOffline(true);
  await send(page);
  await expect(
    page.getByRole("button", { name: "Change answer: For fun" }),
  ).toBeVisible();
  await expect(
    page.getByText("Your answers are saved here.").first(),
  ).toBeVisible();
});

test("rollback: the old form shows when the flag is off", async ({
  page,
}, info) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.addInitScript(() =>
    localStorage.setItem("duolify.guided.onboarding", "off"),
  );
  await page.goto("/profile/setup");
  await expect(page.getByRole("button", { name: "Skip setup" })).toBeVisible();
  await expect(page.locator(".gc")).toHaveCount(0);
  await page.screenshot({ path: info.outputPath("before-old-setup-390.png") });
});
