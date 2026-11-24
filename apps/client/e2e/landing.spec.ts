import { test, expect } from "@playwright/test";
import { test as authed } from "./accounts-fixture";

// Landing page (openspec/changes/landing-page): signed-out web `/` only.
test.beforeEach(({ page }) =>
  page.route("**/v1/auth/refresh", (r) =>
    r.fulfill({ status: 401, body: "{}", contentType: "application/json" }),
  ),
);

for (const [name, size] of [
  ["phone", { width: 390, height: 844 }],
  ["desktop", { width: 1280, height: 800 }],
] as const) {
  test(`signed-out / shows the landing on ${name}`, async ({ page }) => {
    await page.setViewportSize(size);
    await page.goto("/");
    await expect(
      page.getByRole("heading", { name: "Learn anything. Like a game." }),
    ).toBeVisible();
    await expect(page).toHaveURL(/\/$/);
    for (const title of [
      "Play to learn",
      "Interactive video",
      "Your course, by AI",
    ])
      await expect(page.getByRole("heading", { name: title })).toBeVisible();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    await expect(
      page.getByRole("button", { name: "I already have an account" }),
    ).toHaveCount(0);
  });
}

test("video has a poster, does not autoplay and defers its bytes", async ({
  page,
}) => {
  const media: string[] = [];
  page.on("request", (r) => {
    if (/landing\.(mp4|webm)/.test(r.url())) media.push(r.url());
  });
  await page.goto("/");
  const video = page.locator(".landing-video video");
  await expect(video).toHaveAttribute("preload", "none");
  await expect(video).toHaveAttribute("poster", /landing-poster/);
  expect(
    (await page.request.get((await video.getAttribute("poster")) ?? "")).ok(),
  ).toBe(true);
  await expect(video).not.toHaveAttribute("autoplay", /.*/);
  expect(await video.evaluate((v: HTMLVideoElement) => v.paused)).toBe(true);
  await expect(video.locator("track[kind=captions]")).toHaveAttribute(
    "srclang",
    "en",
  );
  // Built without VITE_LANDING_MEDIA_BASE: the poster stands alone, no broken player.
  await expect(page.locator(".landing-play")).toHaveCount(0);
  await expect(video.locator("source")).toHaveCount(0);
  expect(media).toEqual([]);
});

test("theme icon names its action and toggles appearance", async ({ page }) => {
  await page.goto("/");
  const dark = page.getByRole("button", { name: "Switch to dark theme" });
  await expect(dark.locator("svg[data-icon=moon]")).toBeVisible();
  const box = await dark.boundingBox();
  expect(box!.width).toBeGreaterThanOrEqual(44);
  expect(box!.height).toBeGreaterThanOrEqual(44);
  await dark.click();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  const light = page.getByRole("button", { name: "Switch to light theme" });
  await expect(light.locator("svg[data-icon=sun]")).toBeVisible();
  await light.click();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
});

test("Get started and header Sign in open the sign-in chat", async ({
  page,
}) => {
  await page.goto("/");
  await page
    .getByRole("main")
    .getByRole("button", { name: "Get started" })
    .first()
    .click();
  await expect(page.getByLabel("Email address")).toBeVisible();
  await page.goto("/");
  await page
    .getByRole("banner")
    .getByRole("button", { name: "Sign in" })
    .click();
  await expect(page.getByLabel("Email address")).toBeVisible();
});

test("shared course link opens the demo and footer links work", async ({
  page,
}) => {
  await page.goto("/");
  await page.getByRole("button", { name: "Try a shared course" }).click();
  await expect(
    page.getByRole("region", { name: "Shared example" }),
  ).toBeVisible();
});

test("footer links and privacy note", async ({ page }) => {
  await page.goto("/");
  const footer = page.locator(".landing-footer");
  for (const link of ["Privacy", "Terms", "Analytics preference"])
    await expect(footer.getByRole("button", { name: link })).toBeVisible();
  await expect(footer.locator(".landing-note")).toContainText("anonymous");
  await footer.getByRole("button", { name: "Privacy" }).click();
  await expect(page.getByRole("region", { name: "Privacy" })).toBeVisible();
});

test("Arabic mirrors the layout", async ({ page }) => {
  await page.goto("/");
  await page.getByLabel("Language").selectOption("ar");
  await expect(page.locator("html")).toHaveAttribute("dir", "rtl");
  await expect(
    page.getByRole("heading", { name: "تعلّم أي شيء. كأنه لعبة." }),
  ).toBeVisible();
});

authed("signed-in / skips the landing", async ({ page }) => {
  await page.goto("/");
  await expect(page.locator(".landing")).toHaveCount(0);
  await expect(page).not.toHaveURL(/sign-in/);
});
