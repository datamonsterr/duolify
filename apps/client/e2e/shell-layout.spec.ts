// Owner feedback 2026-11-23 (openspec simplify-ux-gestures-hotkeys, D7): no right rail, stats in the
// sidebar, no footnote. API-free: every /v1 call is mocked.
import { expect, test, type Page } from "./native-store-fixture";

const learner = {
  id: "10000000-0000-4000-8000-000000000001",
  display_name: "Learner",
  locale: "en",
  time_zone: "UTC",
  background: "",
  analytics_opt_out: true,
  welcome_state: "completed",
  tour_state: "completed",
};
const wallet = {
  available: 120,
  daily_free: {
    available: 0,
    granted: 46,
    resets_at: "2026-11-24T00:00:00Z",
    timezone: "UTC",
  },
  purchased: 120,
  subscription: 0,
  reserved: 0,
  recent_activity: [],
};
async function seed(page: Page) {
  await page.addInitScript(() =>
    sessionStorage.setItem(
      "duolify.native-store-fixture",
      JSON.stringify({
        access_token: "shell-access",
        refresh_token: "shell-refresh",
        expires_in: 900,
      }),
    ),
  );
  await page.route("**/v1/**", (route) => route.fulfill({ json: [] }));
  await page.route("**/v1/me", (route) => route.fulfill({ json: learner }));
  await page.route("**/v1/gamification", (route) =>
    route.fulfill({ json: { daily_xp: 23, daily_goal: 46 } }),
  );
  await page.route("**/v1/gamification/streak", (route) =>
    route.fulfill({ json: { current_days: 3, today_done: true } }),
  );
  await page.route("**/v1/me/credits", (route) =>
    route.fulfill({ json: wallet }),
  );
}

test.describe("desktop 1280 by 800", () => {
  test.use({ viewport: { width: 1280, height: 800 } });
  test("stats live in the sidebar and the main panel takes the freed width", async ({
    page,
  }) => {
    await seed(page);
    await page.goto("/courses");
    await expect(
      page.getByRole("complementary", { name: "Your progress" }),
    ).toHaveCount(0);
    await expect(page.getByText("A little learning, every day.")).toHaveCount(
      0,
    );
    const stats = page
      .locator(".sidebar")
      .getByRole("group", { name: "Your progress" });
    const streak = stats.getByRole("link", { name: "3 day streak" });
    const xp = stats.getByRole("link", { name: "23 of 46 XP today" });
    const credits = stats.getByRole("link", { name: "120 credits" });
    await expect(streak).toHaveText("3");
    await expect(xp).toContainText("23 / 46");
    await expect(credits).toHaveText("120");
    await expect(streak).toHaveAttribute("href", "/streak");
    await expect(xp).toHaveAttribute("href", "/streak");
    await expect(credits).toHaveAttribute("href", "/credits");
    // Sidebar 224 px; with no 280 px rail the main panel is the rest of the 1280 px frame.
    const main = (await page.locator("main").boundingBox())!;
    expect(main.width).toBeGreaterThan(1280 - 224 - 40);
    // The stats block sits under the nav and above the fold.
    const box = (await stats.boundingBox())!;
    expect(box.y + box.height).toBeLessThan(800);
    await expect(page.getByRole("button", { name: "Shortcuts" })).toBeVisible();
  });

  test("no top bar: the main panel starts with page content", async ({
    page,
  }) => {
    await seed(page);
    await page.goto("/courses");
    await expect(page.locator(".app-bar-row")).toHaveCount(0);
    const main = page.locator("main");
    const heading = main.getByRole("heading", { level: 1 });
    await expect(heading).toBeVisible();
    const m = (await main.boundingBox())!;
    const h = (await heading.boundingBox())!;
    // Only the page's own padding sits above the heading.
    expect(h.y - m.y).toBeLessThanOrEqual(56);
  });

  test("sidebar tools: language menu, theme, shortcuts", async ({ page }) => {
    await seed(page);
    await page.goto("/courses");
    const tools = page.locator(".sidebar-tools");
    const buttons = tools.getByRole("button");
    await expect(buttons).toHaveCount(3);
    for (const name of ["Language", "Dark theme", "Shortcuts"]) {
      const b = tools.getByRole("button", { name, exact: true });
      await expect(b).toHaveText("");
      const box = (await b.boundingBox())!;
      expect(box.width).toBeGreaterThanOrEqual(44);
      expect(box.height).toBeGreaterThanOrEqual(44);
    }
    const language = tools.getByRole("button", { name: "Language" });
    await language.click();
    await expect(
      page.getByRole("menuitemradio", { name: "English" }),
    ).toHaveAttribute("aria-checked", "true");
    await page.keyboard.press("Escape");
    await expect(page.getByRole("menu")).toHaveCount(0);
    await expect(language).toBeFocused();
    await language.click();
    await page.getByRole("menuitemradio", { name: "العربية" }).click();
    await expect(page.locator("html")).toHaveAttribute("lang", "ar");
    await expect(page.locator("html")).toHaveAttribute("dir", "rtl");
    await page.evaluate(() => localStorage.clear());
  });

  test("sidebar tools: theme toggles and shortcuts overlay opens", async ({
    page,
  }) => {
    await seed(page);
    await page.goto("/courses");
    const theme = page.getByRole("button", { name: "Dark theme", exact: true });
    await theme.click();
    await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
    await page.getByRole("button", { name: "Shortcuts", exact: true }).click();
    await expect(
      page.getByRole("dialog", { name: "Keyboard shortcuts" }),
    ).toBeVisible();
  });

  test("a failed read shows a dash and no sentence", async ({ page }) => {
    await seed(page);
    await page.route("**/v1/gamification/streak", (route) =>
      route.fulfill({ status: 500, json: {} }),
    );
    await page.goto("/courses");
    const stats = page
      .locator(".sidebar")
      .getByRole("group", { name: "Your progress" });
    await expect(stats.getByRole("link", { name: /streak/i })).toHaveText("–");
    await expect(stats.getByText(/unavailable/i)).toHaveCount(0);
  });
});

test.describe("phone 390 by 844", () => {
  test.use({ viewport: { width: 390, height: 844 } });
  test("credits stay in the header and the sidebar block is not shown", async ({
    page,
  }) => {
    await seed(page);
    await page.goto("/courses");
    await expect(
      page.getByRole("group", { name: "Your progress" }),
    ).toHaveCount(0);
    await expect(page.locator(".credit-shortcut")).toContainText("120 credits");
    // Owner 2026-11-24: nothing but the logo and credits in the phone header.
    await expect(page.locator(".mobile-header select")).toHaveCount(0);
    await expect(
      page.locator(".mobile-header").getByRole("button"),
    ).toHaveCount(0);
  });

  test("settings: Appearance is a segmented control that applies on tap", async ({
    page,
  }) => {
    await seed(page);
    const settings = {
      learner_id: learner.id,
      analytics_opt_out: true,
      appearance: "system",
      motion: "system",
      narration: true,
      narration_speed: 1,
      captions: true,
      text_size: 1,
      ui_sounds: false,
      haptics: "system",
      locale: "en",
      time_zone: "UTC",
      daily_minutes: null,
      daily_xp: null,
      reminder_enabled: null,
      reminder_time: "",
      fullscreen_on_rotate: true,
      autoplay: true,
      auto_send_after_dictation: true,
      single_key_shortcuts: true,
    };
    await page.route("**/v1/me/settings", (route) =>
      route.fulfill({
        json: route.request().postDataJSON()
          ? { ...settings, ...route.request().postDataJSON().fields }
          : settings,
      }),
    );
    await page.goto("/settings#look");
    const group = page.getByRole("radiogroup", { name: "Appearance" });
    await expect(group).toBeVisible();
    const dark = group.getByRole("radio", { name: "Dark" });
    const box = (await dark.boundingBox())!;
    expect(box.height).toBeGreaterThanOrEqual(44);
    await dark.click();
    await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
    await expect(dark).toHaveAttribute("aria-checked", "true");
  });
});
