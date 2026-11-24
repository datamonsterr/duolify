import { expect, test, type Page } from "./native-store-fixture";

async function seedSettingsAccount(page: Page) {
  let currentSettings = {
    learner_id: "10000000-0000-4000-8000-000000000001",
    analytics_opt_out: false,
    appearance: "system" as const,
    motion: "system" as const,
    narration: true,
    narration_speed: 1,
    captions: true,
    text_size: 1,
    ui_sounds: false,
    haptics: "system" as const,
    locale: "en",
    time_zone: "UTC",
    daily_minutes: 10,
    daily_xp: 20,
    reminder_enabled: false,
    reminder_time: "",
  };

  await page.addInitScript(() => {
    (
      window as unknown as { __vibrations: (number | number[])[] }
    ).__vibrations = [];
    navigator.vibrate = (pattern: number | number[]) => {
      (
        window as unknown as { __vibrations: (number | number[])[] }
      ).__vibrations.push(pattern);
      return true;
    };
    sessionStorage.setItem(
      "duolify.native-store-fixture",
      JSON.stringify({
        access_token: "identity-feedback-access",
        refresh_token: "identity-feedback-refresh",
        expires_in: 900,
      }),
    );
  });

  await page.route("**/v1/me", (route) =>
    route.fulfill({
      json: {
        id: "10000000-0000-4000-8000-000000000001",
        display_name: "Learner",
        locale: "en",
        time_zone: "UTC",
        background: "",
        analytics_opt_out: false,
      },
    }),
  );

  await page.route("**/v1/auth/sessions", (route) =>
    route.fulfill({ json: [] }),
  );

  await page.route("**/v1/me/settings", async (route) => {
    if (route.request().method() === "POST") {
      const body = route.request().postDataJSON();
      currentSettings = { ...currentSettings, ...(body?.fields ?? {}) };
      return route.fulfill({ json: currentSettings });
    }
    return route.fulfill({ json: currentSettings });
  });

  await page.route("**/v1/courses", (route) => route.fulfill({ json: [] }));
}

for (const width of [1280, 360]) {
  test(`approved brand identity and web manifest render at ${width}px`, async ({
    page,
  }) => {
    await seedSettingsAccount(page);
    await page.setViewportSize({ width, height: 800 });
    await page.goto("/");

    // 1. Verify manifest link in HTML head and valid manifest content
    const manifestLink = page.locator('link[rel="manifest"]');
    await expect(manifestLink).toHaveAttribute("href", "/manifest.webmanifest");

    const manifestResponse = await page.request.get("/manifest.webmanifest");
    expect(manifestResponse.status()).toBe(200);
    const manifestJson = await manifestResponse.json();
    expect(manifestJson.name).toBe("Duolify");
    expect(manifestJson.short_name).toBe("Duolify");
    expect(manifestJson.theme_color).toBe("#1f7a0d");
    expect(manifestJson.icons.length).toBeGreaterThan(0);

    // 2. Verify favicon link in HTML head
    const faviconLink = page.locator('link[rel="icon"]');
    await expect(faviconLink).toHaveAttribute("href", "/icon.svg");

    // 3. Verify Brand component, accessible name, and the two geometric tiles
    const brand =
      width >= 768
        ? page.locator('.sidebar a[data-brand="true"]')
        : page.locator('.mobile-header a[data-brand="true"]');
    await expect(brand).toBeVisible();
    await expect(brand).toContainText("duolify");

    const brandMark = brand.locator('[data-brand-mark="true"], .brand-mark');
    await expect(brandMark).toBeVisible();
    await expect(brandMark).toHaveAttribute("aria-hidden", "true");

    // Two tiles: lime (#58C322) and secondary (#1677C9)
    const tiles = brandMark.locator("[data-tile], span");
    await expect(tiles).toHaveCount(2);

    // 4. Verify legibility at small size
    const box = await brandMark.boundingBox();
    expect(box).not.toBeNull();
    expect(box!.width).toBeGreaterThanOrEqual(16);
    expect(box!.height).toBeGreaterThanOrEqual(16);
  });

  test(`optional feedback settings with silent no-op sound and reduced motion at ${width}px`, async ({
    page,
  }) => {
    await seedSettingsAccount(page);
    await page.setViewportSize({ width, height: 800 });

    // Monitor network requests for any audio files (should never be requested)
    const audioRequests: string[] = [];
    page.on("request", (req) => {
      const url = req.url().toLowerCase();
      if (
        url.endsWith(".mp3") ||
        url.endsWith(".wav") ||
        url.endsWith(".ogg") ||
        url.endsWith(".aac") ||
        url.includes("/audio/")
      ) {
        audioRequests.push(url);
      }
    });

    await page.goto("/settings");
    await expect(page.getByRole("heading", { name: "Settings" })).toBeVisible();
    // Phones group controls into sections; a hash opens one.
    await page.goto("/settings#audio");

    // Check UI sounds and Haptics controls
    const soundToggle = page.getByLabel("UI sounds", { exact: true });
    await expect(soundToggle).toBeVisible();
    await expect(soundToggle).not.toBeChecked();

    const hapticsSelect = page.getByLabel("Haptics", { exact: true });
    await expect(hapticsSelect).toBeVisible();
    await expect(hapticsSelect).toHaveValue("system");

    // Toggle UI sounds and set haptics to On
    await soundToggle.check();
    await expect(soundToggle).toBeChecked();
    await hapticsSelect.selectOption("on");

    // Save settings
    const saveButton = page.getByRole("button", { name: "Save settings" });
    const saveNotice = page.locator('.settings-actions [role="status"]');
    await saveButton.click();
    await expect(saveNotice).toHaveText("Settings saved");

    // Verify haptics was called on confirmed button press when haptics=on
    const vibrationsOn = await page.evaluate(
      () =>
        (window as unknown as { __vibrations: (number | number[])[] })
          .__vibrations,
    );
    expect(vibrationsOn.length).toBeGreaterThan(0);

    // Turn haptics off, save, and verify navigator.vibrate is not called
    await hapticsSelect.selectOption("off");
    await saveButton.click();
    await expect(saveNotice).toHaveText("Settings saved");

    // Clear tracked vibrations and click again
    await page.evaluate(() => {
      (
        window as unknown as { __vibrations: (number | number[])[] }
      ).__vibrations = [];
    });
    await saveButton.click();
    await expect(saveNotice).toHaveText("Settings saved");
    const vibrationsOff = await page.evaluate(
      () =>
        (window as unknown as { __vibrations: (number | number[])[] })
          .__vibrations,
    );
    expect(vibrationsOff).toHaveLength(0);

    // Verify silent no-op: no audio requests sent across the wire
    expect(audioRequests).toHaveLength(0);

    // Reduced motion test: select reduce motion and verify static data-motion attribute after saving
    await page.goto("/settings#look");
    const motionToggle = page.getByLabel(/Reduce motion/i);
    await motionToggle.check();
    await saveButton.click();
    await expect(saveNotice).toHaveText("Settings saved");
    await expect(page.locator("html")).toHaveAttribute("data-motion", "reduce");

    // Keyboard navigation parity: test tab focus and keyboard toggle
    await page.goto("/settings#audio");
    await soundToggle.focus();
    await expect(soundToggle).toBeFocused();
    await page.keyboard.press("Space");
    await expect(soundToggle).not.toBeChecked();

    await saveButton.focus();
    await expect(saveButton).toBeFocused();
    await page.keyboard.press("Enter");
    await expect(saveNotice).toHaveText("Settings saved");
  });
}
