import { expect, test } from "@playwright/test";

for (const width of [1280, 320]) {
  test(`locale and appearance preserve guided state at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 950 });
    await page.goto("/guided-chat.html");
    await page
      .getByRole("textbox", { name: "Your answer", exact: true })
      .fill("Private draft العربية");
    await page
      .getByRole("button", { name: "City balcony", exact: true })
      .click();
    await expect(page.getByLabel("Language", { exact: true })).toBeVisible();
    const locale = page.locator('[data-control="locale"]');
    await locale.selectOption("en-XA");
    await expect(page.locator(".guided-composer textarea")).toHaveValue(
      "Private draft العربية",
    );
    await expect(
      page.locator('.guided-actions button[aria-pressed="true"]'),
    ).toHaveCount(1);
    await page.getByLabel(/Appearance/).selectOption("dark");
    await page.getByLabel(/Reduce motion/).check();
    await expect(page.locator("html")).toHaveAttribute("data-motion", "reduce");
    await expect(page.locator(".guided-actions button").first()).toHaveCSS(
      "transform",
      "none",
    );
    await page.evaluate(
      () => (document.documentElement.style.fontSize = "200%"),
    );
    await locale.selectOption("ar");
    await expect(page.locator("html")).toHaveAttribute("dir", "rtl");
    await expect(page).toHaveURL(/guided-chat.html$/);
    const handle = page
      .locator('[data-outline-id="bearings"]')
      .locator(".outline-controls button")
      .first();
    await handle.focus();
    await page.keyboard.press("ArrowDown");
    await expect(handle).toBeFocused();
    await expect(page.locator(".outline-unit").nth(1)).toHaveAttribute(
      "data-outline-id",
      "bearings",
    );
    await expect(page.locator(".guided-composer textarea")).toHaveValue(
      "Private draft العربية",
    );
    await expect
      .poll(() =>
        page.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
      )
      .toBe(true);
    await page.screenshot({
      path: `/tmp/e13-04-${width}-rtl-200.png`,
      fullPage: true,
    });
    await locale.selectOption("en");
    if (width < 768)
      await page
        .getByRole("button", { name: "Open course brief", exact: true })
        .click();
    await page
      .getByRole("textbox", { name: "Goal", exact: true })
      .fill("A private goal");
    await page
      .getByRole("button", { name: "Confirm brief", exact: true })
      .click();
    await expect(
      page.getByText("Brief confirmed", { exact: true }),
    ).toBeVisible();
    if (width < 768) await page.keyboard.press("Escape");
    if (width < 768)
      await page
        .getByRole("button", { name: "Open outline chat", exact: true })
        .click();
    await page.getByRole("button", { name: "Apply", exact: true }).click();
    await expect(
      page.getByText("Proposal applied. Undo available.", { exact: true }),
    ).toBeVisible();
    if (width < 768) await page.keyboard.press("Escape");
    await locale.selectOption("en-XA");
    await expect(page.locator('[data-outline-id="telescope"]')).toBeVisible();
    await expect
      .poll(() =>
        page.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
      )
      .toBe(true);
  });
}

test("system motion overrides user choice and responds live", async ({
  page,
}) => {
  await page.emulateMedia({ reducedMotion: "reduce", colorScheme: "dark" });
  await page.goto("/guided-chat.html");
  await expect(page.locator("html")).toHaveAttribute("data-motion", "reduce");
  await page.getByLabel("Reduce motion", { exact: true }).check();
  await page.getByLabel("Reduce motion", { exact: true }).uncheck();
  await expect(page.locator("html")).toHaveAttribute("data-motion", "reduce");
  await page.emulateMedia({ reducedMotion: "no-preference" });
  await expect(page.locator("html")).toHaveAttribute("data-motion", "full");
});

test("real lesson preserves position, note and static explanation through preferences", async ({
  page,
}) => {
  await page.goto("/lesson/sky");
  await page.getByRole("button", { name: "Next", exact: true }).click();
  await expect(page.getByText("Slide 2 of 3", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Quick note", exact: true }).click();
  await page
    .getByRole("textbox", { name: "Your note", exact: true })
    .fill("Private lesson note");
  await page.locator(".appearance-menu summary").click();
  await page.getByLabel("Appearance", { exact: true }).selectOption("dark");
  await page.getByLabel("Reduce motion", { exact: true }).check();
  await expect(
    page.getByRole("textbox", { name: "Your note", exact: true }),
  ).toHaveValue("Private lesson note");
  await expect(page.getByText("Slide 2 of 3", { exact: true })).toBeVisible();
  const slide = page.frameLocator('iframe[title="Slide 2"]');
  await expect(slide.locator(".star")).toHaveCSS("animation-name", "none");
  await expect(
    slide.getByText("Stars are distant suns, shining across space."),
  ).toBeVisible();
  await page.getByRole("button", { name: "Play", exact: true }).click();
  await expect(slide.locator(".star")).toHaveCSS("animation-name", "none");
  await expect(page).toHaveURL(/\/lesson\/sky$/);
});

for (const width of [1280, 320])
  for (const colorScheme of ["light", "dark"] as const)
    for (const locale of ["ar", "en-XA"]) {
      test(`localized recovery, focus and reflow ${width} ${colorScheme} ${locale}`, async ({
        page,
        context,
      }) => {
        await page.setViewportSize({ width, height: 950 });
        await page.emulateMedia({ colorScheme, reducedMotion: "reduce" });
        await page.goto("/guided-chat.html");
        await page.locator('[data-control="locale"]').selectOption(locale);
        await page.evaluate(
          () => (document.documentElement.style.fontSize = "200%"),
        );
        if (width < 768) {
          await page.locator(".guided-panel-trigger").nth(0).click();
          await page
            .locator(".guided-summary input")
            .nth(1)
            .fill("Private summary retained");
          await expect(
            page.locator(".guided-summary input").nth(1),
          ).toHaveValue("Private summary retained");
          await page.keyboard.press("Escape");
          await expect(
            page.locator(".guided-panel-trigger").nth(0),
          ).toBeFocused();
          await page.locator(".guided-panel-trigger").nth(1).click();
          await expect(page.locator(".outline-proposal")).toBeVisible();
          await page.locator(".outline-proposal button").first().click();
          await page.keyboard.press("Escape");
          await expect(
            page.locator('[data-outline-id="telescope"]'),
          ).toBeVisible();
        }
        const composer = page.locator(".guided-composer textarea");
        await composer.fill("Private draft retained");
        const state = page.getByRole("combobox", {
          name: "Fixture state",
          exact: true,
        });
        await state.selectOption("loading");
        await expect(page.locator(".guided-composer button")).toBeDisabled();
        await state.selectOption("empty");
        await expect(page.locator(".guided-transcript")).not.toBeEmpty();
        await state.selectOption("long");
        await expect(page.locator(".guided-transcript")).toContainText(
          "日本語",
        );
        await expect
          .poll(() =>
            page.evaluate(
              () => document.documentElement.scrollWidth <= innerWidth,
            ),
          )
          .toBe(true);
        await state.selectOption("error");
        await expect(page.locator('.guided-chat [role="alert"]')).toBeVisible();
        await page.locator('.guided-chat [role="alert"] button').click();
        await expect(page.locator('.guided-chat [role="alert"]')).toHaveCount(
          0,
        );
        await context.setOffline(true);
        await expect(
          page.locator(".guided-chat .guided-feedback.warning"),
        ).toBeVisible();
        await expect(composer).toHaveValue("Private draft retained");
        await context.setOffline(false);
        await composer.focus();
        await expect(composer).toHaveCSS("outline-style", "solid");
        const ring = await composer.evaluate((node) =>
          parseFloat(getComputedStyle(node).outlineWidth),
        );
        expect(ring).toBeGreaterThanOrEqual(3);
        const contrast = await page
          .locator('[data-control="appearance"]')
          .evaluate((node) => {
            const style = getComputedStyle(node);
            const lightness = (rgb: string) =>
              rgb
                .match(/[\d.]+/g)!
                .slice(0, 3)
                .map(Number)
                .map((v) => v / 255)
                .map((v) =>
                  v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4,
                )
                .reduce(
                  (sum, v, i) => sum + v * [0.2126, 0.7152, 0.0722][i],
                  0,
                );
            const a = lightness(style.color),
              b = lightness(style.backgroundColor);
            return (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05);
          });
        expect(contrast).toBeGreaterThanOrEqual(4.5);
        const bounds = await composer.boundingBox();
        expect(bounds!.x).toBeGreaterThanOrEqual(0);
        expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(width);
        await page.evaluate(() => {
          const host = window as unknown as { autoScrolls: number };
          host.autoScrolls = 0;
          Element.prototype.scrollIntoView = function () {
            host.autoScrolls++;
          };
          Element.prototype.scrollTo = function () {
            host.autoScrolls++;
          };
        });
        await page.locator(".guided-composer button").click();
        await expect(
          page.locator(".guided-transcript .guided-message.learner"),
        ).toContainText("Private draft retained");
        expect(
          await page.evaluate(
            () => (window as unknown as { autoScrolls: number }).autoScrolls,
          ),
        ).toBe(0);
        await expect(page.locator(".outline-unit").first()).toHaveCSS(
          "animation-name",
          "none",
        );
        await page.screenshot({
          path: `/tmp/e13-04-recovery-${width}-${colorScheme}-${locale}.png`,
          fullPage: true,
        });
      });
    }
