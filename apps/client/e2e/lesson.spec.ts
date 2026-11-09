import { test, expect } from "./accounts-fixture";
import { courses } from "../src/mocks/handlers";

test("a learner opens a course and completes a lesson", async ({ page }) => {
  await page.route("**/courses", (route) => route.fulfill({ json: courses }));
  await page.route("**/courses/astronomy", (route) =>
    route.fulfill({ json: courses[0] }),
  );
  await page.goto("/");
  await page.getByRole("link", { name: /Courses/ }).click();
  await page.getByRole("link", { name: /Everyday astronomy/ }).click();
  await page.getByRole("link", { name: /Start lesson/ }).click();
  await expect(page.getByText("Slide 1 of 3")).toBeVisible();
  const timeline = page.getByRole("slider", { name: "Slide time" });
  await expect(timeline).toBeVisible();
  await expect(timeline).toHaveCSS("accent-color", "rgb(22, 119, 201)");
  await expect(
    page.getByRole("progressbar", { name: "Lesson slides" }).locator("span"),
  ).toHaveCSS("background-color", "rgb(31, 122, 13)");
  await expect(page.getByRole("button", { name: "Speed 1x" })).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Mute narration" }),
  ).toHaveAttribute("aria-pressed", "false");
  await page.getByRole("button", { name: "Pause" }).click();
  const slide = page.frameLocator('iframe[title="Slide 1"]');
  await expect
    .poll(() =>
      slide.locator(".star").evaluate((el) => el.getAnimations()[0].playState),
    )
    .toBe("paused");
  await page.getByRole("button", { name: "Play", exact: true }).click();
  await expect
    .poll(() =>
      slide.locator(".star").evaluate((el) => el.getAnimations()[0].playState),
    )
    .toBe("running");
  await page.getByRole("button", { name: "Speed 1x" }).click();
  await expect(page.getByRole("button", { name: "Speed 1.25x" })).toBeVisible();
  await expect
    .poll(() =>
      slide
        .locator(".star")
        .evaluate((el) => el.getAnimations()[0].playbackRate),
    )
    .toBe(1.25);
  await page.getByRole("button", { name: "Mute narration" }).click();
  await expect(
    page.getByRole("button", { name: "Unmute narration" }),
  ).toHaveAttribute("aria-pressed", "true");
  await page.getByRole("button", { name: "Next" }).click();
  await page.getByRole("button", { name: "Next" }).click();
  await page.getByRole("button", { name: "Finish lesson" }).click();
  await expect(page.getByText("Lesson complete!")).toBeVisible();
});
