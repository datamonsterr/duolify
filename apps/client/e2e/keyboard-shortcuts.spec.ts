import { expect, test, type Page } from "./native-store-fixture";

async function openLearn(page: Page) {
  await page.addInitScript(() =>
    sessionStorage.setItem(
      "duolify.native-store-fixture",
      JSON.stringify({
        access_token: "hotkey-access",
        refresh_token: "hotkey-refresh",
        expires_in: 900,
      }),
    ),
  );
  await page.route("**/v1/**", (route) => route.fulfill({ json: [] }));
  await page.route("**/v1/me", (route) =>
    route.fulfill({
      json: {
        id: "10000000-0000-4000-8000-000000000001",
        display_name: "Learner",
        locale: "en",
        time_zone: "UTC",
        background: "",
        analytics_opt_out: true,
        welcome_state: "completed",
        tour_state: "completed",
      },
    }),
  );
  await page.goto("/");
  await expect(page.locator(".appearance-menu summary")).toBeVisible();
}

test("shows shortcuts, contains focus, and dismisses one layer per Escape", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  await openLearn(page);
  const appearance = page.locator(".appearance-menu");
  await appearance.locator("summary").click();
  const trigger = page.getByRole("button", { name: "Shortcuts", exact: true });
  await trigger.focus();
  await page.keyboard.press("?");
  const help = page.getByRole("dialog", { name: "Keyboard shortcuts" });
  await expect(help).toBeVisible();
  await expect(
    help.getByRole("heading", { name: "App", exact: true }),
  ).toBeVisible();
  const close = help.getByRole("button", { name: "Close", exact: true });
  await expect(close.locator('svg[data-icon="close"]')).toBeVisible();
  const box = await help.boundingBox();
  expect(box).not.toBeNull();
  expect(Math.abs(box!.x + box!.width / 2 - 640)).toBeLessThan(2);
  expect(Math.abs(box!.y + box!.height / 2 - 400)).toBeLessThan(2);
  expect(box!.width).toBeLessThanOrEqual(560);
  const closeBox = await close.boundingBox();
  expect(closeBox!.width).toBeGreaterThanOrEqual(44);
  expect(closeBox!.height).toBeGreaterThanOrEqual(44);
  await expect(close).toBeFocused();
  await page.keyboard.press("Tab");
  await expect(close).toBeFocused();
  await close.hover();
  await expect(help.getByRole("tooltip")).toContainText("Esc");
  const tipBox = await help.getByRole("tooltip").boundingBox();
  expect(tipBox!.x + tipBox!.width).toBeLessThanOrEqual(1280);
  await page.keyboard.press("Escape");
  await expect(help.getByRole("tooltip")).toBeHidden();
  await expect(help).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(help).toBeHidden();
  await expect(trigger).toBeFocused();
  await expect(appearance).toHaveAttribute("open", "");
  await page.keyboard.press("Escape");
  await expect(appearance).not.toHaveAttribute("open", "");
  await expect(page).toHaveURL(/\/$/);
});

test("keeps text entry and composition safe and leaves browser tab keys alone", async ({
  page,
}) => {
  await openLearn(page);
  const text = await page.locator("main").evaluateHandle((main) => {
    const field = document.createElement("textarea");
    main.append(field);
    field.focus();
    return field;
  });
  await page.keyboard.type("kmc?");
  await page.keyboard.press("Shift+Enter");
  expect(await text.evaluate((field) => field.value)).toBe("kmc?\n");
  const help = page.getByRole("dialog", { name: "Keyboard shortcuts" });
  await expect(help).toBeHidden();

  await text.evaluate((field) => field.blur());
  await page.locator("main").dispatchEvent("keydown", {
    key: "?",
    code: "Slash",
    shiftKey: true,
    isComposing: true,
  });
  await expect(help).toBeHidden();
  await page.keyboard.press("Control+2");
  await expect(page).toHaveURL(/\/$/);
  await page.locator("main").dispatchEvent("keydown", {
    key: "?",
    code: "KeyM",
  });
  await expect(help).toBeVisible();
});

test("shows help as a bottom sheet at phone width", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await openLearn(page);
  await page.keyboard.press("?");
  const help = page.getByRole("dialog", { name: "Keyboard shortcuts" });
  await expect(help).toBeVisible();
  const bounds = await help.boundingBox();
  expect(bounds).not.toBeNull();
  expect(bounds!.y + bounds!.height).toBe(844);
  expect(bounds!.x).toBe(0);
  expect(bounds!.width).toBe(390);
  await help.getByRole("button", { name: "Close", exact: true }).click();
  await expect(help).toBeHidden();
});
